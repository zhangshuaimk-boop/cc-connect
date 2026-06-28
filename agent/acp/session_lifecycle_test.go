package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chenhg5/cc-connect/core"
)

func TestAgentStartSessionSendCancelAndClose(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	workDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "helper.jsonl")
	a, err := New(map[string]any{
		"cmd":          os.Args[0],
		"args":         append(acpHelperArgs("lifecycle"), "--stdio"),
		"env":          map[string]any{"GO_WANT_ACP_HELPER_PROCESS": "1", "ACP_HELPER_LOG": logPath, "STATIC_ONLY": "1"},
		"auth_method":  "test_login",
		"display_name": "Test ACP",
		"work_dir":     workDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	agent := a.(*Agent)
	agent.SetSessionEnv([]string{"SESSION_ONLY=1"})
	agent.SetMode("Plan")

	if got := agent.Name(); got != "acp" {
		t.Fatalf("Name = %q, want acp", got)
	}
	if got := agent.CLIBinaryName(); got != filepath.Base(os.Args[0]) {
		t.Fatalf("CLIBinaryName = %q, want %q", got, filepath.Base(os.Args[0]))
	}
	if got := agent.GetWorkDir(); got != workDir {
		t.Fatalf("GetWorkDir = %q, want %q", got, workDir)
	}

	sess, err := agent.StartSession(ctx, "resume-123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sess.Alive() {
			_ = sess.Close()
		}
	})

	if got := sess.CurrentSessionID(); got != "loaded-resume-123" {
		t.Fatalf("CurrentSessionID = %q, want loaded-resume-123", got)
	}
	if got := agent.GetMode(); got != "Plan" {
		t.Fatalf("agent GetMode = %q, want pending Plan", got)
	}
	if modes := agent.PermissionModes(); len(modes) != 2 || modes[1].Key != "plan" {
		t.Fatalf("PermissionModes = %+v, want normal/plan", modes)
	}
	live, ok := sess.(interface{ CurrentMode() string })
	if !ok {
		t.Fatal("session does not expose CurrentMode")
	}
	if got := live.CurrentMode(); got != "plan" {
		t.Fatalf("CurrentMode = %q, want plan", got)
	}

	err = sess.Send("hello", []core.ImageAttachment{{MimeType: "image/png", Data: []byte("png")}}, []core.FileAttachment{{
		FileName: "note.txt",
		MimeType: "text/plain",
		Data:     []byte("file-body"),
	}})
	if err != nil {
		t.Fatal(err)
	}

	text := waitACPEvent(t, sess.Events(), core.EventText)
	if text.Content != "streamed hello" || text.SessionID != "loaded-resume-123" {
		t.Fatalf("text event = %+v", text)
	}
	result := waitACPEvent(t, sess.Events(), core.EventResult)
	if !result.Done || result.SessionID != "loaded-resume-123" {
		t.Fatalf("result event = %+v", result)
	}

	cancellable, ok := sess.(interface{ CancelTurn() error })
	if !ok {
		t.Fatal("session does not expose CancelTurn")
	}
	if err := cancellable.CancelTurn(); err != nil {
		t.Fatal(err)
	}
	waitACPHelperLogContains(t, logPath, "method", "session/cancel")
	if !sess.Alive() {
		t.Fatal("session should be alive before Close")
	}
	if err := sess.Close(); err != nil {
		t.Fatal(err)
	}
	if sess.Alive() {
		t.Fatal("session should not be alive after Close")
	}

	logs := readACPHelperLogs(t, logPath)
	assertACPHelperLogContains(t, logs, "args", "--stdio")
	assertACPHelperLogContains(t, logs, "env", "STATIC_ONLY=1")
	assertACPHelperLogContains(t, logs, "env", "SESSION_ONLY=1")
	assertACPHelperLogContains(t, logs, "method", "initialize")
	assertACPHelperLogContains(t, logs, "method", "authenticate")
	assertACPHelperLogContains(t, logs, "method", "session/load")
	assertACPHelperLogContains(t, logs, "method", "session/set_mode")
	assertACPHelperLogContains(t, logs, "method", "session/prompt")
	assertACPHelperLogContains(t, logs, "method", "session/cancel")
	assertACPHelperLogContains(t, logs, "prompt", "hello")
	assertACPHelperLogContains(t, logs, "prompt", "note.txt")
	assertACPHelperLogContains(t, logs, "prompt", ".cc-connect")
}

func TestAgentListSessionsUsesProbeAndCwdFilter(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	workDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "helper.jsonl")
	a, err := New(map[string]any{
		"cmd":      os.Args[0],
		"args":     acpHelperArgs("list"),
		"env":      map[string]string{"GO_WANT_ACP_HELPER_PROCESS": "1", "ACP_HELPER_LOG": logPath, "ACP_HELPER_WORKDIR": workDir},
		"work_dir": workDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	agent := a.(*Agent)

	sessions, err := agent.ListSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("got %d sessions, want 2 matching/cwd-less sessions: %+v", len(sessions), sessions)
	}
	if sessions[0].ID != "match" || sessions[0].Summary != "Matching workspace" {
		t.Fatalf("sessions[0] = %+v", sessions[0])
	}
	if sessions[0].ModifiedAt.IsZero() {
		t.Fatalf("ModifiedAt was not parsed: %+v", sessions[0])
	}
	if sessions[1].ID != "cwdless" {
		t.Fatalf("sessions[1] = %+v, want cwdless entry", sessions[1])
	}

	logs := readACPHelperLogs(t, logPath)
	assertACPHelperLogContains(t, logs, "method", "initialize")
	assertACPHelperLogContains(t, logs, "method", "session/list")
	assertACPHelperLogContains(t, logs, "cwd", workDir)
}

func TestAgentListSessionsUnsupportedCachesResult(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	a, err := New(map[string]any{
		"cmd":      os.Args[0],
		"args":     acpHelperArgs("no-list"),
		"env":      map[string]string{"GO_WANT_ACP_HELPER_PROCESS": "1"},
		"work_dir": t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	agent := a.(*Agent)

	sessions, err := agent.ListSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if sessions != nil {
		t.Fatalf("sessions = %+v, want nil for unsupported session/list", sessions)
	}
	if !agent.listUnsupported.Load() {
		t.Fatal("listUnsupported should be cached after initialize without list capability")
	}
}

func TestAgentStartSessionFallsBackWhenLoadFails(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	a, err := New(map[string]any{
		"cmd":      os.Args[0],
		"args":     acpHelperArgs("load-fails"),
		"env":      map[string]string{"GO_WANT_ACP_HELPER_PROCESS": "1"},
		"work_dir": t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := a.(*Agent).StartSession(ctx, "old-session")
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	if got := sess.CurrentSessionID(); got != "new-after-load-failure" {
		t.Fatalf("CurrentSessionID = %q, want new-after-load-failure", got)
	}
}

func TestNewACPSessionReportsHandshakeErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := newACPSession(ctx, acpSessionConfig{
		command: os.Args[0],
		args:    acpHelperArgs("bad-initialize"),
		extraEnv: []string{
			"GO_WANT_ACP_HELPER_PROCESS=1",
		},
		workDir: t.TempDir(),
	})
	if err == nil || !strings.Contains(err.Error(), "parse initialize result") {
		t.Fatalf("err = %v, want parse initialize result", err)
	}

	_, err = newACPSession(ctx, acpSessionConfig{
		command: os.Args[0],
		args:    acpHelperArgs("empty-session"),
		extraEnv: []string{
			"GO_WANT_ACP_HELPER_PROCESS=1",
		},
		workDir: t.TempDir(),
	})
	if err == nil || !strings.Contains(err.Error(), "empty sessionId") {
		t.Fatalf("err = %v, want empty sessionId", err)
	}
}

func TestSessionPermissionRequestAndResponses(t *testing.T) {
	s, wResp, rReq := newTestSession(t, nil)
	defer wResp.Close()
	defer rReq.Close()

	s.cacheToolCallInput(json.RawMessage(`{
		"sessionId":"test-session-id",
		"update":{
			"sessionUpdate":"tool_call_update",
			"toolCallId":"tc1",
			"rawInput":{"command":"rm -rf /tmp/nope","description":"dangerous command"}
		}
	}`))
	s.onServerRequest("session/request_permission", json.RawMessage(`"perm-1"`), json.RawMessage(`{
		"sessionId":"test-session-id",
		"toolCall":{"toolCallId":"tc1","title":"Run shell","kind":"bash","rawInput":{}},
		"options":[
			{"optionId":"allow-once","kind":"allow_once","name":"Allow"},
			{"optionId":"reject-once","kind":"reject_once","name":"Reject"}
		]
	}`))

	ev := waitACPEvent(t, s.Events(), core.EventPermissionRequest)
	if ev.RequestID != "perm-1" || ev.ToolName != "Run shell" {
		t.Fatalf("permission event = %+v", ev)
	}
	if ev.ToolInput != "# dangerous command\nrm -rf /tmp/nope" {
		t.Fatalf("ToolInput = %q", ev.ToolInput)
	}
	if ev.ToolInputRaw == nil {
		t.Fatalf("ToolInputRaw not captured: %+v", ev)
	}

	respCh := readACPRPCLineAsync(rReq)
	if err := s.RespondPermission("perm-1", core.PermissionResult{Behavior: "allow"}); err != nil {
		t.Fatal(err)
	}
	resp := waitACPRPCLine(t, respCh)
	if !strings.Contains(resp, `"id":"perm-1"`) || !strings.Contains(resp, `"optionId":"allow-once"`) {
		t.Fatalf("allow response = %s", resp)
	}

	if err := s.RespondPermission("perm-1", core.PermissionResult{Behavior: "deny"}); err == nil {
		t.Fatal("expected unknown request error after permission state is consumed")
	}
}

func TestSessionPermissionAllowWithoutOptionsRespondsError(t *testing.T) {
	s, wResp, rReq := newTestSession(t, nil)
	defer wResp.Close()
	defer rReq.Close()

	s.onServerRequest("session/request_permission", json.RawMessage(`7`), json.RawMessage(`{
		"sessionId":"test-session-id",
		"toolCall":{"toolCallId":"tc2","kind":"write","rawInput":{"file_path":"/tmp/a.txt"}}
	}`))
	_ = waitACPEvent(t, s.Events(), core.EventPermissionRequest)

	respCh := readACPRPCLineAsync(rReq)
	if err := s.RespondPermission("7", core.PermissionResult{Behavior: "allow"}); err != nil {
		t.Fatal(err)
	}
	resp := waitACPRPCLine(t, respCh)
	if !strings.Contains(resp, `"id":7`) || !strings.Contains(resp, `"error"`) || !strings.Contains(resp, "no permission options") {
		t.Fatalf("error response = %s", resp)
	}
}

func TestSessionServerRequestDispatchAndNotification(t *testing.T) {
	s, wResp, rReq := newTestSession(t, nil)
	defer wResp.Close()
	defer rReq.Close()

	cursorRespCh := readACPRPCLineAsync(rReq)
	s.onServerRequest("cursor/task", json.RawMessage(`11`), json.RawMessage(`{}`))
	if resp := waitACPRPCLine(t, cursorRespCh); !strings.Contains(resp, `"id":11`) || !strings.Contains(resp, `"result"`) {
		t.Fatalf("cursor ack = %s", resp)
	}

	unknownRespCh := readACPRPCLineAsync(rReq)
	s.onServerRequest("vendor/unknown", json.RawMessage(`12`), json.RawMessage(`{}`))
	if resp := waitACPRPCLine(t, unknownRespCh); !strings.Contains(resp, `"id":12`) || !strings.Contains(resp, `"error"`) {
		t.Fatalf("unknown response = %s", resp)
	}

	s.onNotification("session/update", json.RawMessage(`{
		"sessionId":"test-session-id",
		"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"from notification"}}
	}`))
	ev := waitACPEvent(t, s.Events(), core.EventText)
	if ev.Content != "from notification" {
		t.Fatalf("notification event = %+v", ev)
	}
}

func TestSessionCancelTurnAndSendErrors(t *testing.T) {
	s, _, _ := newTestSession(t, nil)

	s.setACPSessionID("")
	if err := s.CancelTurn(); err == nil || !strings.Contains(err.Error(), "no active session") {
		t.Fatalf("CancelTurn err = %v, want no active session", err)
	}
	if err := s.Send("hello", nil, nil); err == nil || !strings.Contains(err.Error(), "no agent session id") {
		t.Fatalf("Send err = %v, want no agent session id", err)
	}

	s.alive.Store(false)
	if err := s.Send("hello", nil, nil); err == nil || !strings.Contains(err.Error(), "session closed") {
		t.Fatalf("Send after closed err = %v, want session closed", err)
	}
	if err := s.RespondPermission("missing", core.PermissionResult{Behavior: "deny"}); err == nil || !strings.Contains(err.Error(), "session closed") {
		t.Fatalf("RespondPermission after closed err = %v, want session closed", err)
	}
}

func waitACPEvent(t *testing.T, events <-chan core.Event, typ core.EventType) core.Event {
	t.Helper()
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for {
		select {
		case ev := <-events:
			if ev.Type == typ {
				return ev
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for event type %s", typ)
		}
	}
}

func readACPRPCLineAsync(r io.Reader) <-chan string {
	done := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(r)
		if sc.Scan() {
			done <- sc.Text()
			return
		}
		done <- ""
	}()
	return done
}

func waitACPRPCLine(t *testing.T, done <-chan string) string {
	t.Helper()
	select {
	case line := <-done:
		if line == "" {
			t.Fatal("empty RPC line")
		}
		return line
	case <-time.After(2 * time.Second):
		t.Fatal("timed out reading RPC line")
	}
	return ""
}

func acpHelperArgs(scenario string) []string {
	return []string{"-test.run=TestACPHelperProcess", "--", scenario}
}

func readACPHelperLogs(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	return lines
}

func assertACPHelperLogContains(t *testing.T, logs []string, key string, want string) {
	t.Helper()
	needle := `"` + key + `":`
	for _, line := range logs {
		if strings.Contains(line, needle) && strings.Contains(line, want) {
			return
		}
	}
	t.Fatalf("missing helper log %s containing %q in:\n%s", key, want, strings.Join(logs, "\n"))
}

func waitACPHelperLogContains(t *testing.T, path string, key string, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		logs := readACPHelperLogs(t, path)
		needle := `"` + key + `":`
		for _, line := range logs {
			if strings.Contains(line, needle) && strings.Contains(line, want) {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	logs := readACPHelperLogs(t, path)
	t.Fatalf("missing helper log %s containing %q in:\n%s", key, want, strings.Join(logs, "\n"))
}

func TestACPHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_ACP_HELPER_PROCESS") != "1" {
		return
	}
	scenario := "lifecycle"
	for i, arg := range os.Args {
		if arg == "--" && i+1 < len(os.Args) {
			scenario = os.Args[i+1]
			break
		}
	}
	runACPHelperProcess(scenario)
	os.Exit(0)
}

func runACPHelperProcess(scenario string) {
	logPath := os.Getenv("ACP_HELPER_LOG")
	logHelperRecord(logPath, map[string]any{
		"kind": "args",
		"args": strings.Join(os.Args, " "),
	})
	logHelperRecord(logPath, map[string]any{
		"kind": "env",
		"env":  strings.Join(acpHelperRelevantEnv(), "\n"),
	})

	sc := bufio.NewScanner(os.Stdin)
	enc := json.NewEncoder(os.Stdout)
	for sc.Scan() {
		var req struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params"`
		}
		if json.Unmarshal(sc.Bytes(), &req) != nil || req.Method == "" {
			continue
		}
		logHelperRecord(logPath, map[string]any{
			"kind":   "method",
			"method": req.Method,
			"params": string(req.Params),
		})
		if isJSONRPCIDNullOrAbsent(req.ID) {
			continue
		}

		switch req.Method {
		case "initialize":
			switch scenario {
			case "bad-initialize":
				writeACPResult(enc, req.ID, "not an object")
			case "no-list":
				writeACPResult(enc, req.ID, map[string]any{
					"protocolVersion": 1,
					"agentCapabilities": map[string]any{
						"loadSession":         true,
						"sessionCapabilities": map[string]any{},
					},
				})
			default:
				writeACPResult(enc, req.ID, map[string]any{
					"protocolVersion": 1,
					"agentCapabilities": map[string]any{
						"loadSession": true,
						"sessionCapabilities": map[string]any{
							"list": map[string]any{},
						},
					},
				})
			}
		case "authenticate":
			writeACPResult(enc, req.ID, map[string]any{})
		case "session/load":
			if scenario == "load-fails" {
				writeACPError(enc, req.ID, -32000, "load failed")
				continue
			}
			writeACPResult(enc, req.ID, map[string]any{
				"sessionId": "loaded-resume-123",
				"modes":     acpHelperModes("normal"),
			})
		case "session/new":
			if scenario == "empty-session" {
				writeACPResult(enc, req.ID, map[string]any{"sessionId": ""})
				continue
			}
			sessionID := "new-session"
			if scenario == "load-fails" {
				sessionID = "new-after-load-failure"
			}
			writeACPResult(enc, req.ID, map[string]any{
				"sessionId": sessionID,
				"modes":     acpHelperModes("normal"),
			})
		case "session/set_mode":
			writeACPResult(enc, req.ID, map[string]any{})
		case "session/prompt":
			var params struct {
				SessionID string `json:"sessionId"`
				Prompt    []struct {
					Text string `json:"text"`
				} `json:"prompt"`
			}
			_ = json.Unmarshal(req.Params, &params)
			if len(params.Prompt) > 0 {
				logHelperRecord(logPath, map[string]any{
					"kind":   "prompt",
					"prompt": params.Prompt[0].Text,
				})
			}
			_ = enc.Encode(map[string]any{
				"jsonrpc": "2.0",
				"method":  "session/update",
				"params": map[string]any{
					"sessionId": params.SessionID,
					"update": map[string]any{
						"sessionUpdate": "agent_message_chunk",
						"content":       map[string]any{"type": "text", "text": "streamed hello"},
					},
				},
			})
			writeACPResult(enc, req.ID, map[string]any{})
		case "session/list":
			cwd := os.Getenv("ACP_HELPER_WORKDIR")
			if cwd == "" {
				cwd = "/tmp/acp-helper"
			}
			logHelperRecord(logPath, map[string]any{
				"kind": "cwd",
				"cwd":  cwd,
			})
			writeACPResult(enc, req.ID, map[string]any{
				"sessions": []map[string]any{
					{"sessionId": "match", "cwd": cwd, "title": "Matching workspace", "updatedAt": "2026-04-18T16:15:29+00:00"},
					{"sessionId": "other", "cwd": filepath.Join(cwd, "other"), "title": "Other workspace"},
					{"sessionId": "cwdless", "title": "No cwd"},
				},
			})
		default:
			writeACPError(enc, req.ID, -32601, "unknown method")
		}
	}
}

func acpHelperModes(current string) map[string]any {
	return map[string]any{
		"currentModeId": current,
		"availableModes": []map[string]any{
			{"id": "normal", "name": "Code", "description": "Write code"},
			{"id": "plan", "name": "Plan", "description": "Plan only"},
		},
	}
}

func writeACPResult(enc *json.Encoder, id json.RawMessage, result any) {
	_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id), "result": result})
}

func writeACPError(enc *json.Encoder, id json.RawMessage, code int, message string) {
	_ = enc.Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(id),
		"error":   map[string]any{"code": code, "message": message},
	})
}

func logHelperRecord(path string, rec map[string]any) {
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		return
	}
	defer f.Close()
	_ = json.NewEncoder(f).Encode(rec)
}

func acpHelperRelevantEnv() []string {
	keys := []string{
		"GO_WANT_ACP_HELPER_PROCESS",
		"ACP_HELPER_LOG",
		"ACP_HELPER_WORKDIR",
		"STATIC_ONLY",
		"SESSION_ONLY",
	}
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		if value, ok := os.LookupEnv(key); ok {
			out = append(out, key+"="+value)
		}
	}
	return out
}
