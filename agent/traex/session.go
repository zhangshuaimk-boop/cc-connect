package traex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/chenhg5/cc-connect/core"
)

type traexSession struct {
	workDir       string
	model         string
	effort        string
	mode          string
	baseURL       string
	modelProvider string
	instructions  string
	cliBin        string
	cliExtraArgs  []string
	extraEnv      []string
	events        chan core.Event
	threadID      atomic.Value // stores string
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	alive         atomic.Bool
	closeOnce     sync.Once
	cmdMu         sync.Mutex
	cmds          map[*exec.Cmd]struct{}

	pendingMsgs []string

	contextMu    sync.RWMutex
	contextUsage *core.ContextUsage
	sessionFile  string
}

var traexSessionCloseTimeout = 8 * time.Second
var traexSessionForceKillWait = 2 * time.Second
var traexContextUsageRetryDelay = 50 * time.Millisecond
var traexContextUsageRetryCount = 4

func newTraexSession(ctx context.Context, cliBin string, cliExtraArgs []string, workDir, model, effort, mode, resumeID, baseURL string, extraEnv []string, modelProvider string, instructions string) (*traexSession, error) {
	sessionCtx, cancel := context.WithCancel(ctx)

	ts := &traexSession{
		workDir:       workDir,
		model:         model,
		effort:        effort,
		mode:          mode,
		baseURL:       baseURL,
		modelProvider: modelProvider,
		instructions:  strings.TrimSpace(instructions),
		cliBin:        cliBin,
		cliExtraArgs:  cliExtraArgs,
		extraEnv:      extraEnv,
		events:        make(chan core.Event, 64),
		ctx:           sessionCtx,
		cancel:        cancel,
		cmds:          make(map[*exec.Cmd]struct{}),
	}
	ts.alive.Store(true)

	if resumeID != "" && resumeID != core.ContinueSession {
		ts.threadID.Store(resumeID)
	}

	return ts, nil
}

func buildTraexInstructions(systemPrompt, platformPrompt, appendPrompt string) string {
	var parts []string
	if systemPrompt = strings.TrimSpace(systemPrompt); systemPrompt != "" {
		parts = append(parts, "Project system prompt:\n"+systemPrompt)
	}
	if platformPrompt = strings.TrimSpace(platformPrompt); platformPrompt != "" {
		parts = append(parts, "## Formatting\n"+platformPrompt)
	}
	if appendPrompt = strings.TrimSpace(appendPrompt); appendPrompt != "" {
		parts = append(parts, "Additional project instructions:\n"+appendPrompt)
	}
	return strings.Join(parts, "\n\n")
}

func traexConfigString(key, value string) string {
	return key + "=" + strconv.Quote(value)
}

func (ts *traexSession) Send(prompt string, images []core.ImageAttachment, files []core.FileAttachment) error {
	if len(files) > 0 {
		filePaths := core.SaveFilesToDisk(ts.workDir, files)
		prompt = core.AppendFileRefs(prompt, filePaths)
	}
	if !ts.alive.Load() {
		return fmt.Errorf("session is closed")
	}

	prompt, imagePaths, err := ts.stageImages(prompt, images)
	if err != nil {
		return err
	}

	isResume := ts.CurrentSessionID() != ""
	args := ts.buildExecArgs(prompt, imagePaths)
	if len(ts.cliExtraArgs) > 0 {
		args = append(append([]string{}, ts.cliExtraArgs...), args...)
	}

	bin := ts.cliBin
	if bin == "" {
		bin = "traex"
	}

	slog.Debug("traexSession: launching", "resume", isResume, "args", core.RedactArgs(args))

	cmd := exec.CommandContext(ts.ctx, bin, args...)
	cmd.Dir = ts.workDir
	prepareCmdForKill(cmd)
	if len(ts.extraEnv) > 0 {
		cmd.Env = core.MergeEnv(os.Environ(), ts.extraEnv)
	}
	cmd.Stdin = strings.NewReader(prompt)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("traexSession: stdout pipe: %w", err)
	}

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("traexSession: start: %w", err)
	}
	ts.addCmd(cmd)

	ts.wg.Add(1)
	go ts.readLoop(cmd, stdout, &stderrBuf)

	return nil
}

func (ts *traexSession) stageImages(prompt string, images []core.ImageAttachment) (string, []string, error) {
	if len(images) == 0 {
		return prompt, nil, nil
	}

	imgDir := filepath.Join(ts.workDir, ".cc-connect", "images")
	if err := os.MkdirAll(imgDir, 0o755); err != nil {
		return "", nil, fmt.Errorf("traexSession: create image dir: %w", err)
	}

	imagePaths := make([]string, 0, len(images))
	for i, img := range images {
		ext := traexImageExt(img.MimeType)
		fname := fmt.Sprintf("img_%d_%d%s", time.Now().UnixMilli(), i, ext)
		fpath := filepath.Join(imgDir, fname)
		if err := os.WriteFile(fpath, img.Data, 0o644); err != nil {
			return "", nil, fmt.Errorf("traexSession: save image: %w", err)
		}
		imagePaths = append(imagePaths, fpath)
	}

	if strings.TrimSpace(prompt) == "" {
		prompt = "Please analyze the attached image(s)."
	}

	return prompt, imagePaths, nil
}

func traexImageExt(mime string) string {
	switch mime {
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	default:
		return ".png"
	}
}

func (ts *traexSession) buildExecArgs(prompt string, imagePaths []string) []string {
	tid := ts.CurrentSessionID()
	isResume := tid != ""

	var args []string
	if isResume {
		args = []string{"exec", "resume", "--skip-git-repo-check"}
	} else {
		args = []string{"exec", "--skip-git-repo-check"}
	}

	switch ts.mode {
	case "auto-edit", "full-auto":
		args = append(args, "--permission-mode", "bypass_permissions")
	case "yolo":
		args = append(args, "--dangerously-bypass-approvals-and-sandbox")
	case "plan":
		args = append(args, "--permission-mode", "plan")
	}

	if ts.model != "" {
		args = append(args, "--model", ts.model)
	}
	if ts.modelProvider != "" {
		args = append(args, "-c", fmt.Sprintf("model_provider=%q", ts.modelProvider))
	}
	if ts.baseURL != "" {
		args = append(args, "-c", fmt.Sprintf("openai_base_url=%q", ts.baseURL))
	}
	if ts.instructions != "" {
		args = append(args, "-c", traexConfigString("instructions", ts.instructions))
	}

	if isResume {
		args = append(args, tid)
		for _, imagePath := range imagePaths {
			args = append(args, "--image", imagePath)
		}
		args = append(args, "--json", "-")
	} else {
		for _, imagePath := range imagePaths {
			args = append(args, "--image", imagePath)
		}
		args = append(args, "--json", "--cd", ts.workDir, "-")
	}
	return args
}

func (ts *traexSession) readLoop(cmd *exec.Cmd, stdout io.ReadCloser, stderrBuf *bytes.Buffer) {
	defer ts.wg.Done()
	defer func() {
		defer ts.removeCmd(cmd)
		if err := cmd.Wait(); err != nil {
			stderrMsg := strings.TrimSpace(stderrBuf.String())
			if stderrMsg != "" {
				slog.Error("traexSession: process failed", "error", err, "stderr", stderrMsg)
				evt := core.Event{Type: core.EventError, Error: fmt.Errorf("%s", stderrMsg)}
				select {
				case ts.events <- evt:
				case <-ts.ctx.Done():
					return
				}
			}
		}
	}()

	if err := readJSONLines(stdout, func(line []byte) error {
		lineText := string(line)
		if lineText == "" {
			return nil
		}

		slog.Debug("traexSession: raw", "line", truncate(lineText, 500))

		var raw map[string]any
		if err := json.Unmarshal(line, &raw); err != nil {
			slog.Debug("traexSession: non-JSON line", "line", lineText)
			return nil
		}

		ts.handleEvent(raw)
		return nil
	}); err != nil {
		slog.Error("traexSession: read stdout error", "error", err)
		evt := core.Event{Type: core.EventError, Error: fmt.Errorf("read stdout: %w", err)}
		select {
		case ts.events <- evt:
		case <-ts.ctx.Done():
			return
		}
	}
}

func readJSONLines(r io.Reader, handle func([]byte) error) error {
	reader := bufio.NewReader(r)

	for {
		line, err := reader.ReadBytes('\n')
		if errors.Is(err, io.EOF) && len(line) == 0 {
			return nil
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}

		line = bytes.TrimRight(line, "\r\n")
		if len(line) > 0 {
			if err := handle(line); err != nil {
				return err
			}
		}

		if errors.Is(err, io.EOF) {
			return nil
		}
	}
}

func (ts *traexSession) handleEvent(raw map[string]any) {
	eventType, _ := raw["type"].(string)

	switch eventType {
	case "thread.started":
		if tid, ok := raw["thread_id"].(string); ok {
			ts.threadID.Store(tid)
			ts.contextMu.Lock()
			ts.sessionFile = ""
			ts.contextUsage = nil
			ts.contextMu.Unlock()
			slog.Debug("traexSession: thread started", "thread_id", tid)
		}

	case "turn.started":
		ts.pendingMsgs = ts.pendingMsgs[:0]
		ts.contextMu.Lock()
		ts.contextUsage = nil
		ts.contextMu.Unlock()
		slog.Debug("traexSession: turn started")

	case "item.started":
		ts.handleItemStarted(raw)

	case "item.completed":
		ts.handleItemCompleted(raw)

	case "turn.completed":
		ts.refreshContextUsageFromRollout()
		ts.flushPendingAsText()
		evt := core.Event{Type: core.EventResult, SessionID: ts.CurrentSessionID(), Done: true}
		select {
		case ts.events <- evt:
		case <-ts.ctx.Done():
			return
		}

	case "turn.failed":
		errMsg := ""
		if errObj, ok := raw["error"].(map[string]any); ok {
			errMsg, _ = errObj["message"].(string)
		}
		if errMsg == "" {
			errMsg = "turn failed (no details)"
		}
		slog.Warn("traexSession: turn failed", "error", errMsg)
		evt := core.Event{Type: core.EventError, Error: fmt.Errorf("%s", errMsg)}
		select {
		case ts.events <- evt:
		case <-ts.ctx.Done():
			return
		}

	case "error":
		msg, _ := raw["message"].(string)
		if strings.Contains(msg, "Reconnecting") || strings.Contains(msg, "Falling back") {
			slog.Debug("traexSession: transient error", "message", msg)
		} else {
			slog.Warn("traexSession: error event", "message", msg)
		}

	default:
		slog.Debug("traexSession: unhandled event type", "type", eventType)
	}
}

func (ts *traexSession) flushPendingAsThinking() {
	if ts.ctx.Err() != nil {
		return
	}
	for _, text := range ts.pendingMsgs {
		if ts.ctx.Err() != nil {
			return
		}
		evt := core.Event{Type: core.EventThinking, Content: text}
		select {
		case ts.events <- evt:
		case <-ts.ctx.Done():
			return
		}
	}
	ts.pendingMsgs = ts.pendingMsgs[:0]
}

func (ts *traexSession) flushPendingAsText() {
	if ts.ctx.Err() != nil {
		return
	}
	for _, text := range ts.pendingMsgs {
		if ts.ctx.Err() != nil {
			return
		}
		evt := core.Event{Type: core.EventText, Content: text}
		select {
		case ts.events <- evt:
		case <-ts.ctx.Done():
			return
		}
	}
	ts.pendingMsgs = ts.pendingMsgs[:0]
}

var traexToolNames = map[string]string{
	"web_search":       "WebSearch",
	"file_search":      "FileSearch",
	"code_interpreter": "CodeInterpreter",
	"computer_use":     "ComputerUse",
	"mcp_tool":         "MCP",
}

func (ts *traexSession) handleItemStarted(raw map[string]any) {
	item, ok := raw["item"].(map[string]any)
	if !ok {
		slog.Debug("traexSession: item.started missing item field")
		return
	}
	itemType, _ := item["type"].(string)
	slog.Debug("traexSession: item.started", "item_type", itemType)

	if itemType == "agent_message" || itemType == "message" || itemType == "reasoning" {
		return
	}

	ts.flushPendingAsThinking()

	switch itemType {
	case "command_execution":
		command, _ := item["command"].(string)
		evt := core.Event{Type: core.EventToolUse, ToolName: "Bash", ToolInput: command}
		select {
		case ts.events <- evt:
		case <-ts.ctx.Done():
			return
		}
	case "function_call":
		name, _ := item["name"].(string)
		args, _ := item["arguments"].(string)
		evt := core.Event{Type: core.EventToolUse, ToolName: name, ToolInput: args}
		select {
		case ts.events <- evt:
		case <-ts.ctx.Done():
			return
		}
	}
}

func (ts *traexSession) handleItemCompleted(raw map[string]any) {
	item, ok := raw["item"].(map[string]any)
	if !ok {
		slog.Debug("traexSession: item.completed missing item field")
		return
	}
	itemType, _ := item["type"].(string)
	slog.Debug("traexSession: item.completed", "item_type", itemType)

	switch itemType {
	case "reasoning":
		text := extractItemText(item, "summary", "summary_text")
		if text != "" {
			evt := core.Event{Type: core.EventThinking, Content: text}
			select {
			case ts.events <- evt:
			case <-ts.ctx.Done():
				return
			}
		}

	case "agent_message", "message":
		text := extractItemText(item, "content", "output_text")
		if text != "" {
			ts.pendingMsgs = append(ts.pendingMsgs, text)
		}

	case "command_execution":
		command, _ := item["command"].(string)
		status, _ := item["status"].(string)
		output, _ := item["aggregated_output"].(string)
		exitCode, _ := item["exit_code"].(float64)
		code := int(exitCode)
		success := toolSuccess(status, &code)

		slog.Debug("traexSession: command completed",
			"command", truncate(command, 100),
			"status", status,
			"exit_code", code,
			"output_len", len(output),
		)
		evt := core.Event{
			Type:         core.EventToolResult,
			ToolName:     "Bash",
			ToolResult:   truncate(strings.TrimSpace(output), 500),
			ToolStatus:   strings.TrimSpace(status),
			ToolExitCode: &code,
			ToolSuccess:  &success,
		}
		select {
		case ts.events <- evt:
		case <-ts.ctx.Done():
			return
		}

	case "function_call":
		name, _ := item["name"].(string)
		status, _ := item["status"].(string)
		output, _ := item["output"].(string)
		success := toolSuccess(status, nil)
		slog.Debug("traexSession: function_call completed",
			"name", name, "status", status, "output_len", len(output),
		)
		evt := core.Event{
			Type:        core.EventToolResult,
			ToolName:    name,
			ToolResult:  truncate(strings.TrimSpace(output), 500),
			ToolStatus:  strings.TrimSpace(status),
			ToolSuccess: &success,
		}
		select {
		case ts.events <- evt:
		case <-ts.ctx.Done():
			return
		}

	case "function_call_output":
		slog.Debug("traexSession: function_call_output")

	case "error":
		msg, _ := item["message"].(string)
		if msg != "" && !strings.Contains(msg, "Falling back") {
			slog.Warn("traexSession: item error", "message", msg)
		}

	default:
		if toolName, known := traexToolNames[itemType]; known {
			input := extractToolInput(item)
			evt := core.Event{Type: core.EventToolUse, ToolName: toolName, ToolInput: input}
			select {
			case ts.events <- evt:
			case <-ts.ctx.Done():
				return
			}
		} else {
			slog.Debug("traexSession: unhandled item type", "item_type", itemType)
		}
	}
}

func extractToolInput(item map[string]any) string {
	if action, ok := item["action"].(map[string]any); ok {
		if queries, ok := action["queries"].([]any); ok && len(queries) > 0 {
			var parts []string
			for _, q := range queries {
				if s, ok := q.(string); ok && s != "" {
					parts = append(parts, s)
				}
			}
			if len(parts) > 0 {
				return strings.Join(parts, "\n")
			}
		}
		if q, _ := action["query"].(string); q != "" {
			return q
		}
	}
	if q, _ := item["query"].(string); q != "" {
		return q
	}
	if n, _ := item["name"].(string); n != "" {
		return n
	}
	return ""
}

func toolSuccess(status string, exitCode *int) bool {
	s := strings.ToLower(strings.TrimSpace(status))
	if exitCode != nil {
		return *exitCode == 0
	}
	return s == "completed" || s == "success" || s == "succeeded" || s == "ok"
}

func (ts *traexSession) RespondPermission(_ string, _ core.PermissionResult) error {
	return nil
}

func (ts *traexSession) Events() <-chan core.Event {
	return ts.events
}

func (ts *traexSession) CurrentSessionID() string {
	v, _ := ts.threadID.Load().(string)
	return v
}

func (ts *traexSession) GetWorkDir() string {
	return ts.workDir
}

func (ts *traexSession) GetModel() string {
	return strings.TrimSpace(ts.model)
}

func (ts *traexSession) GetReasoningEffort() string {
	return strings.TrimSpace(ts.effort)
}

func (ts *traexSession) Alive() bool {
	return ts.alive.Load()
}

func (ts *traexSession) GetContextUsage() *core.ContextUsage {
	ts.contextMu.RLock()
	defer ts.contextMu.RUnlock()
	return cloneContextUsage(ts.contextUsage)
}

func (ts *traexSession) refreshContextUsageFromRollout() {
	sessionID := strings.TrimSpace(ts.CurrentSessionID())
	if sessionID == "" {
		return
	}

	for attempt := 0; attempt < traexContextUsageRetryCount; attempt++ {
		ts.contextMu.RLock()
		cachedPath := ts.sessionFile
		ts.contextMu.RUnlock()

		usage, path, err := loadContextUsageFromRollout(sessionID, cachedPath)
		if err == nil && usage != nil {
			ts.contextMu.Lock()
			ts.sessionFile = path
			ts.contextUsage = cloneContextUsage(usage)
			ts.contextMu.Unlock()
			return
		}

		if attempt == traexContextUsageRetryCount-1 {
			if err != nil {
				slog.Debug("traexSession: context usage unavailable", "thread_id", sessionID, "error", err)
			}
			return
		}

		select {
		case <-time.After(traexContextUsageRetryDelay):
		case <-ts.ctx.Done():
			return
		}
	}
}

func (ts *traexSession) Close() error {
	ts.alive.Store(false)
	ts.cancel()
	done := make(chan struct{})
	go func() {
		ts.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		ts.closeOnce.Do(func() {
			close(ts.events)
		})
		return nil
	case <-time.After(traexSessionCloseTimeout):
		cmds := ts.activeCmds()
		slog.Warn("traexSession: graceful close timed out, killing active process groups",
			"wait", traexSessionCloseTimeout,
			"count", len(cmds))
		if err := forceKillAllCmds(cmds); err != nil {
			slog.Debug("traexSession: force kill failed", "error", err)
		}
		select {
		case <-done:
			ts.closeOnce.Do(func() {
				close(ts.events)
			})
			return nil
		case <-time.After(traexSessionForceKillWait):
			slog.Warn("traexSession: force kill wait timed out, deferring events channel close until readLoop exits",
				"wait", traexSessionForceKillWait)
			go func() {
				<-done
				ts.closeOnce.Do(func() {
					close(ts.events)
				})
			}()
			return nil
		}
	}
}

func (ts *traexSession) addCmd(cmd *exec.Cmd) {
	ts.cmdMu.Lock()
	defer ts.cmdMu.Unlock()
	ts.cmds[cmd] = struct{}{}
}

func (ts *traexSession) removeCmd(cmd *exec.Cmd) {
	ts.cmdMu.Lock()
	defer ts.cmdMu.Unlock()
	delete(ts.cmds, cmd)
}

func (ts *traexSession) activeCmds() []*exec.Cmd {
	ts.cmdMu.Lock()
	defer ts.cmdMu.Unlock()
	cmds := make([]*exec.Cmd, 0, len(ts.cmds))
	for cmd := range ts.cmds {
		cmds = append(cmds, cmd)
	}
	return cmds
}

func forceKillAllCmds(cmds []*exec.Cmd) error {
	var errs []error
	for _, cmd := range cmds {
		if err := forceKillCmd(cmd); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func extractItemText(item map[string]any, arrayField, elementType string) string {
	if arr, ok := item[arrayField].([]any); ok {
		var parts []string
		for _, elem := range arr {
			m, ok := elem.(map[string]any)
			if !ok {
				continue
			}
			if elementType != "" {
				if t, _ := m["type"].(string); t != elementType {
					continue
				}
			}
			if t, _ := m["text"].(string); t != "" {
				parts = append(parts, t)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
	}
	text, _ := item["text"].(string)
	return text
}

func truncate(s string, maxRunes int) string {
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	return string([]rune(s)[:maxRunes]) + "..."
}
