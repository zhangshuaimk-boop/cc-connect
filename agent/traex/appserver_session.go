package traex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"

	"github.com/chenhg5/cc-connect/core"
)

// TraeX exec-server protocol probe, traecli 0.200.14:
//
//   $ echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"clientName":"cc-connect-probe"}}' \
//       | traex exec-server --listen stdio://
//   {"id":1,"result":{"sessionId":"..."}}
//
// The same binary returns JSON-RPC -32601 for the methods cc-connect needs to
// run a persistent app_server backend:
//
//   initialized  -> exec-server stub does not implement `initialized` yet
//   thread/start -> exec-server stub does not implement `thread/start` yet
//   thread/list  -> exec-server stub does not implement `thread/list` yet
//
// Strings in the binary mention thread/start, thread/resume, turn/start, and
// permission schemas, but the stdio exec-server surface is still a stub. Keep
// the config/backend skeleton aligned with codex, fail fast for app_server, and
// fill in the real session implementation after TraeX exposes turn and approval
// RPC methods.

type traexRPCResponseEnvelope struct {
	ID     any             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *traexRPCError  `json:"error"`
}

type traexRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type traexInitializeResponse struct {
	SessionID string `json:"sessionId"`
}

type traexAppServerSession struct {
	url       string
	workDir   string
	model     string
	effort    string
	events    chan core.Event
	threadID  atomic.Value
	alive     atomic.Bool
	closeOnce atomic.Bool
}

const (
	appServerRequestTimeout      = 120 * time.Second
	appServerUsageRefreshTimeout = 1500 * time.Millisecond
)

func newTraexAppServerSession(ctx context.Context, cliBin string, cliExtraArgs []string, url, workDir, model, effort, mode, resumeID, baseURL string, extraEnv []string, modelProvider string) (*traexAppServerSession, error) {
	if strings.TrimSpace(url) == "" {
		url = "stdio://"
	}
	if url != "stdio://" && !strings.EqualFold(url, "stdio") {
		return nil, fmt.Errorf("traex app_server backend currently only supports stdio:// probing; got %q", url)
	}
	if len(cliExtraArgs) > 0 {
		return nil, fmt.Errorf("traex app_server backend does not support cmd extra args yet: %s", strings.Join(cliExtraArgs, " "))
	}
	if cliBin == "" {
		cliBin = "traex"
	}

	if err := probeTraexExecServer(ctx, cliBin, workDir, extraEnv); err != nil {
		return nil, err
	}

	// This point is intentionally unreachable with traecli 0.200.14. Leave a
	// concrete session value so tests can assert the AgentSession contract while
	// the app_server implementation is filled in after TraeX ships the RPCs.
	s := &traexAppServerSession{
		url:     "stdio://",
		workDir: workDir,
		model:   model,
		effort:  effort,
		events:  make(chan core.Event),
	}
	if resumeID != "" && resumeID != core.ContinueSession {
		s.threadID.Store(resumeID)
	}
	s.alive.Store(true)
	return s, nil
}

func probeTraexExecServer(ctx context.Context, cliBin, workDir string, extraEnv []string) error {
	probeCtx, cancel := context.WithTimeout(ctx, appServerUsageRefreshTimeout)
	defer cancel()

	cmd := exec.CommandContext(probeCtx, cliBin, "exec-server", "--listen", "stdio://")
	cmd.Dir = workDir
	if len(extraEnv) > 0 {
		cmd.Env = core.MergeEnv(os.Environ(), extraEnv)
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("traex app_server stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("traex app_server stdout pipe: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("traex app_server start: %w", err)
	}

	writeReq := func(id int64, method string, params any) error {
		req := map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"method":  method,
		}
		if params != nil {
			req["params"] = params
		}
		if err := json.NewEncoder(stdin).Encode(req); err != nil {
			return fmt.Errorf("traex app_server write %s: %w", method, err)
		}
		return nil
	}

	reader := bufio.NewReader(stdout)
	readResp := func(id int64) (traexRPCResponseEnvelope, error) {
		for {
			line, err := reader.ReadBytes('\n')
			if err != nil {
				return traexRPCResponseEnvelope{}, fmt.Errorf("traex app_server read response: %w", err)
			}
			line = bytes.TrimSpace(line)
			if len(line) == 0 {
				continue
			}
			var resp traexRPCResponseEnvelope
			if err := json.Unmarshal(line, &resp); err != nil {
				continue
			}
			if responseIDMatches(resp.ID, id) {
				return resp, nil
			}
		}
	}

	if err := writeReq(1, "initialize", map[string]any{"clientName": "cc-connect"}); err != nil {
		_ = terminateProbe(cmd, stdin)
		return err
	}
	initResp, err := readResp(1)
	if err != nil {
		_ = terminateProbe(cmd, stdin)
		return err
	}
	if initResp.Error != nil {
		_ = terminateProbe(cmd, stdin)
		return fmt.Errorf("traex app_server initialize failed: %s", initResp.Error.Message)
	}
	var initResult traexInitializeResponse
	if err := json.Unmarshal(initResp.Result, &initResult); err != nil {
		_ = terminateProbe(cmd, stdin)
		return fmt.Errorf("traex app_server initialize response: %w", err)
	}

	if err := writeReq(2, "thread/start", map[string]any{"cwd": workDir}); err != nil {
		_ = terminateProbe(cmd, stdin)
		return err
	}
	threadResp, err := readResp(2)
	if err != nil {
		_ = terminateProbe(cmd, stdin)
		return err
	}

	_ = terminateProbe(cmd, stdin)
	if threadResp.Error != nil {
		if threadResp.Error.Code == -32601 {
			return fmt.Errorf("traex app_server backend requires traex CLI with exec-server thread RPC support; current %s implements initialize only and returned: %s. Use backend=\"exec\" or upgrade traex", cliBin, threadResp.Error.Message)
		}
		return fmt.Errorf("traex app_server thread/start probe failed: %s", threadResp.Error.Message)
	}
	return errors.New("traex app_server thread/start probe unexpectedly succeeded; implement persistent session RPC handling before enabling this backend")
}

func terminateProbe(cmd *exec.Cmd, stdin io.WriteCloser) error {
	_ = stdin.Close()
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	return cmd.Wait()
}

func responseIDMatches(raw any, want int64) bool {
	switch v := raw.(type) {
	case float64:
		return int64(v) == want
	case int64:
		return v == want
	case int:
		return int64(v) == want
	case json.Number:
		n, err := v.Int64()
		return err == nil && n == want
	default:
		return false
	}
}

func (s *traexAppServerSession) Send(_ string, _ []core.ImageAttachment, _ []core.FileAttachment) error {
	return errors.New("traex app_server backend is not implemented by current traex exec-server")
}

func (s *traexAppServerSession) RespondPermission(_ string, _ core.PermissionResult) error {
	return errors.New("traex app_server backend is not implemented by current traex exec-server")
}

func (s *traexAppServerSession) Events() <-chan core.Event { return s.events }

func (s *traexAppServerSession) CurrentSessionID() string {
	v, _ := s.threadID.Load().(string)
	return v
}

func (s *traexAppServerSession) Alive() bool { return s.alive.Load() }

func (s *traexAppServerSession) Close() error {
	if s.closeOnce.CompareAndSwap(false, true) {
		s.alive.Store(false)
		close(s.events)
	}
	return nil
}
