package traex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/chenhg5/cc-connect/core"
)

func TestNormalizeMode(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", "default"},
		{"default", "default"},
		{"yolo", "yolo"},
		{"YOLO", "yolo"},
		{"bypass", "yolo"},
		{"full-auto", "full-auto"},
		{"auto", "full-auto"},
		{"auto-edit", "auto-edit"},
		{"plan", "plan"},
		{"ask", "ask"},
	}
	for _, tt := range tests {
		got := normalizeMode(tt.input)
		if got != tt.expected {
			t.Errorf("normalizeMode(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestNormalizeBackendAndAppServerURL(t *testing.T) {
	backendTests := []struct {
		input    string
		expected string
	}{
		{"", "exec"},
		{"exec", "exec"},
		{"app_server", "app_server"},
		{"app-server", "app_server"},
		{"appserver", "app_server"},
		{"ws", "app_server"},
		{"unknown", "exec"},
	}
	for _, tt := range backendTests {
		if got := normalizeBackend(tt.input); got != tt.expected {
			t.Errorf("normalizeBackend(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}

	urlTests := []struct {
		input    string
		expected string
	}{
		{"", "stdio://"},
		{"stdio", "stdio://"},
		{"stdio://", "stdio://"},
		{"ws://127.0.0.1:3946", "ws://127.0.0.1:3946"},
	}
	for _, tt := range urlTests {
		if got := normalizeAppServerURL(tt.input); got != tt.expected {
			t.Errorf("normalizeAppServerURL(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestAgentBasicConfigProviderAndOptions(t *testing.T) {
	t.Setenv("TRAE_HOME", t.TempDir())

	agent := &Agent{
		workDir:         ".",
		model:           "fallback-model",
		reasoningEffort: "low",
		mode:            "default",
		backend:         "app_server",
		appServerURL:    "stdio://",
		cliBin:          "traex",
		activeIdx:       -1,
		configEnv:       []string{"CONFIG_ENV=one"},
	}

	if agent.Name() != "traex" {
		t.Fatalf("Name() = %q, want traex", agent.Name())
	}

	workDir := t.TempDir()
	agent.SetWorkDir(workDir)
	if got := agent.GetWorkDir(); got != workDir {
		t.Fatalf("GetWorkDir() = %q, want %q", got, workDir)
	}
	if got := agent.ProjectMemoryFile(); got != filepath.Join(workDir, "AGENTS.md") {
		t.Fatalf("ProjectMemoryFile() = %q", got)
	}
	if got := agent.GlobalMemoryFile(); got != filepath.Join(os.Getenv("TRAE_HOME"), "AGENTS.md") {
		t.Fatalf("GlobalMemoryFile() = %q", got)
	}
	if got := agent.CompressCommand(); got != "" {
		t.Fatalf("CompressCommand() = %q, want empty", got)
	}
	if err := agent.Stop(); err != nil {
		t.Fatalf("Stop() = %v", err)
	}

	agent.SetModel("manual-model")
	if got := agent.GetModel(); got != "manual-model" {
		t.Fatalf("GetModel() before provider = %q", got)
	}
	agent.SetReasoningEffort("MED")
	if got := agent.GetReasoningEffort(); got != "medium" {
		t.Fatalf("GetReasoningEffort() = %q, want medium", got)
	}
	agent.SetReasoningEffort("minimal")
	if got := agent.GetReasoningEffort(); got != "" {
		t.Fatalf("invalid reasoning effort should clear to empty, got %q", got)
	}
	if got := agent.AvailableReasoningEfforts(); strings.Join(got, ",") != "low,medium,high,xhigh" {
		t.Fatalf("AvailableReasoningEfforts() = %v", got)
	}

	agent.SetMode("AUTO_EDIT")
	if got := agent.GetMode(); got != "auto-edit" {
		t.Fatalf("GetMode() = %q, want auto-edit", got)
	}
	opts := agent.WorkspaceAgentOptions()
	if opts["mode"] != "auto-edit" || opts["model"] != "manual-model" || opts["backend"] != "app_server" || opts["app_server_url"] != "stdio://" {
		t.Fatalf("WorkspaceAgentOptions() = %#v", opts)
	}

	providers := []core.ProviderConfig{
		{Name: "unused", Model: "unused-model"},
		{
			Name:    "active",
			APIKey:  "secret",
			BaseURL: "https://api.example.com",
			Model:   "provider-model",
			Models:  []core.ModelOption{{Name: "provider-model", Desc: "configured"}},
			Env:     map[string]string{"EXTRA_PROVIDER_ENV": "yes"},
		},
	}
	agent.SetProviders(providers)
	if !agent.SetActiveProvider("active") {
		t.Fatal("SetActiveProvider(active) = false")
	}
	if agent.SetActiveProvider("missing") {
		t.Fatal("SetActiveProvider(missing) = true")
	}
	if got := agent.GetModel(); got != "provider-model" {
		t.Fatalf("GetModel() with provider = %q, want provider-model", got)
	}
	if got := agent.AvailableModels(context.Background()); len(got) != 1 || got[0].Name != "provider-model" {
		t.Fatalf("AvailableModels() = %#v", got)
	}
	active := agent.GetActiveProvider()
	if active == nil || active.Name != "active" {
		t.Fatalf("GetActiveProvider() = %#v", active)
	}
	listed := agent.ListProviders()
	listed[1].Name = "mutated"
	if got := agent.ListProviders()[1].Name; got != "active" {
		t.Fatalf("ListProviders() returned mutable internal slice, got provider %q", got)
	}
	env := agent.providerEnvLocked()
	for _, want := range []string{"OPENAI_API_KEY=secret", "OPENAI_BASE_URL=https://api.example.com", "EXTRA_PROVIDER_ENV=yes"} {
		if !containsString(env, want) {
			t.Fatalf("provider env missing %q in %v", want, env)
		}
	}
	if !agent.SetActiveProvider("") {
		t.Fatal("SetActiveProvider(empty) = false")
	}
	if got := agent.GetActiveProvider(); got != nil {
		t.Fatalf("GetActiveProvider() after clear = %#v, want nil", got)
	}

	modes := agent.PermissionModes()
	if len(modes) != 5 || modes[0].Key != "default" || modes[len(modes)-1].Key != "yolo" {
		t.Fatalf("PermissionModes() = %#v", modes)
	}
}

func TestNewUsesCmdOptionsAndRejectsMissingCLI(t *testing.T) {
	if _, err := New(map[string]any{"cmd": "definitely-missing-traex-binary"}); err == nil {
		t.Fatal("New() with missing CLI returned nil error")
	}

	binDir := t.TempDir()
	writeFakeTraexScript(t, binDir, `#!/bin/sh
exit 0
`)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	got, err := New(map[string]any{
		"work_dir":         "/tmp/project",
		"model":            "gpt-test",
		"reasoning_effort": "x-high",
		"mode":             "auto",
		"backend":          "app-server",
		"app_server_url":   "stdio",
		"cmd":              "traex --profile dev",
		"env":              map[string]any{"CONFIG_A": "one", "IGNORED": 2},
	})
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	agent := got.(*Agent)
	if agent.workDir != "/tmp/project" || agent.model != "gpt-test" || agent.reasoningEffort != "xhigh" || agent.mode != "full-auto" {
		t.Fatalf("agent fields = %#v", agent)
	}
	if agent.backend != "app_server" || agent.appServerURL != "stdio://" {
		t.Fatalf("backend fields = backend=%q appServerURL=%q", agent.backend, agent.appServerURL)
	}
	if agent.cliBin != "traex" || strings.Join(agent.cliExtraArgs, " ") != "--profile dev" {
		t.Fatalf("cli parsed as bin=%q extra=%v", agent.cliBin, agent.cliExtraArgs)
	}
	if !containsString(agent.configEnv, "CONFIG_A=one") || containsString(agent.configEnv, "IGNORED=2") {
		t.Fatalf("configEnv = %v", agent.configEnv)
	}
}

func TestStartSessionAppServerFailsFastWhenExecServerIsStub(t *testing.T) {
	binDir := t.TempDir()
	writeFakeTraexScript(t, binDir, `#!/bin/sh
if [ "$1" = "exec-server" ]; then
  IFS= read -r init_req
  printf '%s\n' '{"id":1,"result":{"sessionId":"probe-session"}}'
  IFS= read -r thread_req
  printf '%s\n' '{"id":2,"error":{"code":-32601,"message":"exec-server stub does not implement thread/start yet"}}'
  sleep 1
  exit 0
fi
exit 0
`)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	got, err := New(map[string]any{
		"work_dir":       t.TempDir(),
		"backend":        "app_server",
		"app_server_url": "stdio",
	})
	if err != nil {
		t.Fatalf("New() = %v", err)
	}

	_, err = got.StartSession(context.Background(), "")
	if err == nil {
		t.Fatal("StartSession(app_server) returned nil error")
	}
	if !strings.Contains(err.Error(), "requires traex CLI with exec-server thread RPC support") {
		t.Fatalf("StartSession(app_server) error = %v", err)
	}
}

func TestStartSessionExecBackendKeepsExistingSessionPath(t *testing.T) {
	agent := &Agent{
		workDir:         t.TempDir(),
		model:           "gpt-test",
		reasoningEffort: "high",
		mode:            "plan",
		backend:         "exec",
		cliBin:          "traex",
		configEnv:       []string{"CONFIG_ENV=1"},
		activeIdx:       -1,
	}

	sess, err := agent.StartSession(context.Background(), "thread-existing")
	if err != nil {
		t.Fatalf("StartSession(exec) = %v", err)
	}
	defer sess.Close()

	ts, ok := sess.(*traexSession)
	if !ok {
		t.Fatalf("StartSession(exec) returned %T, want *traexSession", sess)
	}
	if got := ts.CurrentSessionID(); got != "thread-existing" {
		t.Fatalf("CurrentSessionID() = %q, want thread-existing", got)
	}
	if got := ts.GetModel(); got != "gpt-test" {
		t.Fatalf("GetModel() = %q, want gpt-test", got)
	}
	if got := ts.GetReasoningEffort(); got != "high" {
		t.Fatalf("GetReasoningEffort() = %q, want high", got)
	}
	if !containsString(ts.extraEnv, "CONFIG_ENV=1") {
		t.Fatalf("extraEnv = %v, want CONFIG_ENV=1", ts.extraEnv)
	}
}

func TestHandleEventThreadStarted(t *testing.T) {
	ts := &traexSession{
		events: make(chan core.Event, 1),
		ctx:    context.Background(),
	}
	raw := map[string]any{
		"type":      "thread.started",
		"thread_id": "thread-abc-123",
	}
	ts.handleEvent(raw)

	if ts.CurrentSessionID() != "thread-abc-123" {
		t.Errorf("Expected thread_id 'thread-abc-123', got %q", ts.CurrentSessionID())
	}
}

func TestHandleEventItemCompletedAgentMessage(t *testing.T) {
	ts := &traexSession{
		events: make(chan core.Event, 16),
		ctx:    context.Background(),
	}
	raw := map[string]any{
		"type": "item.completed",
		"item": map[string]any{
			"id":   "item_1",
			"type": "agent_message",
			"text": "hello world",
		},
	}
	ts.handleEvent(raw)

	if len(ts.pendingMsgs) != 1 {
		t.Fatalf("Expected 1 pending msg, got %d", len(ts.pendingMsgs))
	}
	if ts.pendingMsgs[0] != "hello world" {
		t.Errorf("Expected 'hello world', got %q", ts.pendingMsgs[0])
	}
}

func TestHandleEventTurnCompleted(t *testing.T) {
	ts := &traexSession{
		events:      make(chan core.Event, 16),
		ctx:         context.Background(),
		pendingMsgs: []string{"hello world"},
	}
	ts.threadID.Store("thread-abc-123")

	raw := map[string]any{
		"type": "turn.completed",
	}
	ts.handleEvent(raw)

	// Should emit EventResult with Done=true
	select {
	case evt := <-ts.events:
		if evt.Type != core.EventText {
			t.Errorf("Expected EventText, got %v", evt.Type)
		}
		if evt.Content != "hello world" {
			t.Errorf("Expected 'hello world', got %q", evt.Content)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for text event")
	}

	select {
	case evt := <-ts.events:
		if evt.Type != core.EventResult {
			t.Errorf("Expected EventResult, got %v", evt.Type)
		}
		if !evt.Done {
			t.Error("Expected Done=true")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for result event")
	}
}

func TestHandleEventTurnFailedAndTransientErrors(t *testing.T) {
	ts := &traexSession{
		events: make(chan core.Event, 4),
		ctx:    context.Background(),
	}

	ts.handleEvent(map[string]any{
		"type":  "turn.failed",
		"error": map[string]any{"message": "quota exceeded"},
	})
	evt := requireEvent(t, ts.events, core.EventError)
	if evt.Error == nil || evt.Error.Error() != "quota exceeded" {
		t.Fatalf("turn.failed error = %v", evt.Error)
	}

	ts.handleEvent(map[string]any{"type": "turn.failed"})
	evt = requireEvent(t, ts.events, core.EventError)
	if evt.Error == nil || evt.Error.Error() != "turn failed (no details)" {
		t.Fatalf("turn.failed fallback error = %v", evt.Error)
	}

	ts.handleEvent(map[string]any{"type": "error", "message": "Reconnecting to server"})
	ts.handleEvent(map[string]any{"type": "unknown"})
	assertNoEvent(t, ts.events)
}

func TestItemStartedAndCompletedEvents(t *testing.T) {
	ts := &traexSession{
		events:      make(chan core.Event, 16),
		ctx:         context.Background(),
		pendingMsgs: []string{"thinking first"},
	}

	ts.handleEvent(map[string]any{"type": "item.started", "item": map[string]any{
		"type":    "command_execution",
		"command": "go test ./agent/traex",
	}})
	thinking := requireEvent(t, ts.events, core.EventThinking)
	if thinking.Content != "thinking first" {
		t.Fatalf("thinking content = %q", thinking.Content)
	}
	toolUse := requireEvent(t, ts.events, core.EventToolUse)
	if toolUse.ToolName != "Bash" || toolUse.ToolInput != "go test ./agent/traex" {
		t.Fatalf("command tool use = %#v", toolUse)
	}

	ts.handleEvent(map[string]any{"type": "item.started", "item": map[string]any{
		"type":      "function_call",
		"name":      "apply_patch",
		"arguments": `{"file":"x"}`,
	}})
	funcUse := requireEvent(t, ts.events, core.EventToolUse)
	if funcUse.ToolName != "apply_patch" || funcUse.ToolInput != `{"file":"x"}` {
		t.Fatalf("function tool use = %#v", funcUse)
	}

	ts.handleEvent(map[string]any{"type": "item.completed", "item": map[string]any{
		"type": "reasoning",
		"summary": []any{
			map[string]any{"type": "summary_text", "text": "step one"},
			map[string]any{"type": "summary_text", "text": "step two"},
		},
	}})
	reasoning := requireEvent(t, ts.events, core.EventThinking)
	if reasoning.Content != "step one\nstep two" {
		t.Fatalf("reasoning content = %q", reasoning.Content)
	}

	ts.handleEvent(map[string]any{"type": "item.completed", "item": map[string]any{
		"type":              "command_execution",
		"status":            "failed",
		"aggregated_output": strings.Repeat("x", 600),
		"exit_code":         float64(2),
	}})
	result := requireEvent(t, ts.events, core.EventToolResult)
	if result.ToolName != "Bash" || result.ToolStatus != "failed" || result.ToolExitCode == nil || *result.ToolExitCode != 2 {
		t.Fatalf("command result = %#v", result)
	}
	if result.ToolSuccess == nil || *result.ToolSuccess {
		t.Fatalf("command success = %#v", result.ToolSuccess)
	}
	if !strings.HasSuffix(result.ToolResult, "...") {
		t.Fatalf("command output was not truncated: len=%d", len(result.ToolResult))
	}

	ts.handleEvent(map[string]any{"type": "item.completed", "item": map[string]any{
		"type":   "function_call",
		"name":   "web_search",
		"status": "completed",
		"output": "found",
	}})
	funcResult := requireEvent(t, ts.events, core.EventToolResult)
	if funcResult.ToolName != "web_search" || funcResult.ToolResult != "found" || funcResult.ToolSuccess == nil || !*funcResult.ToolSuccess {
		t.Fatalf("function result = %#v", funcResult)
	}

	ts.handleEvent(map[string]any{"type": "item.completed", "item": map[string]any{
		"type": "web_search",
		"action": map[string]any{
			"queries": []any{"one", "two"},
		},
	}})
	webUse := requireEvent(t, ts.events, core.EventToolUse)
	if webUse.ToolName != "WebSearch" || webUse.ToolInput != "one\ntwo" {
		t.Fatalf("web search use = %#v", webUse)
	}
}

func TestBuildExecArgsFresh(t *testing.T) {
	ts := &traexSession{
		workDir:       "/tmp/test",
		model:         "claude-sonnet-4",
		mode:          "yolo",
		modelProvider: "llmproxy",
		baseURL:       "https://api.example.com",
	}
	args := ts.buildExecArgs("hello", nil)

	argStr := strings.Join(args, " ")
	if !strings.Contains(argStr, "exec") {
		t.Error("missing exec subcommand")
	}
	if !strings.Contains(argStr, "--json") {
		t.Error("missing --json flag")
	}
	if !strings.Contains(argStr, "--dangerously-bypass-approvals-and-sandbox") {
		t.Error("missing yolo flag")
	}
	if !strings.Contains(argStr, "--cd") {
		t.Error("missing --cd flag for fresh session")
	}
	if strings.Contains(argStr, "resume") {
		t.Error("should not contain resume for fresh session")
	}
	if args[len(args)-1] != "-" {
		t.Errorf("last arg should be '-', got %q", args[len(args)-1])
	}
}

func TestBuildExecArgsResume(t *testing.T) {
	ts := &traexSession{
		workDir: "/tmp/test",
		model:   "claude-sonnet-4",
		mode:    "default",
	}
	ts.threadID.Store("thread-abc-123")
	args := ts.buildExecArgs("hello", nil)

	argStr := strings.Join(args, " ")
	if !strings.Contains(argStr, "resume") {
		t.Error("missing resume subcommand")
	}
	if !strings.Contains(argStr, "thread-abc-123") {
		t.Error("missing thread ID")
	}
	if strings.Contains(argStr, "--cd") {
		t.Error("should not contain --cd for resume")
	}
}

func TestBuildExecArgsAutoEdit(t *testing.T) {
	ts := &traexSession{
		workDir: "/tmp/test",
		mode:    "auto-edit",
	}
	args := ts.buildExecArgs("hello", nil)
	argStr := strings.Join(args, " ")
	if !strings.Contains(argStr, "--permission-mode") {
		t.Error("missing --permission-mode for auto-edit")
	}
	if !strings.Contains(argStr, "bypass_permissions") {
		t.Error("missing bypass_permissions value")
	}
}

func TestBuildExecArgsPlan(t *testing.T) {
	ts := &traexSession{
		workDir: "/tmp/test",
		mode:    "plan",
	}
	args := ts.buildExecArgs("hello", nil)
	argStr := strings.Join(args, " ")
	if !strings.Contains(argStr, "--permission-mode") {
		t.Error("missing --permission-mode for plan")
	}
	if !strings.Contains(argStr, "plan") {
		t.Error("missing plan value")
	}
}

func TestBuildExecArgsResumeImagesAndProvider(t *testing.T) {
	ts := &traexSession{
		workDir:       "/tmp/test",
		model:         "gpt-5.5",
		mode:          "full-auto",
		modelProvider: "shengsuanyun",
		baseURL:       "https://router.example.com/api/v1",
	}
	ts.threadID.Store("thread-resume")
	args := ts.buildExecArgs("hello", []string{"/tmp/a.png", "/tmp/b.jpg"})

	for _, want := range [][]string{
		{"exec", "resume", "--skip-git-repo-check"},
		{"--permission-mode", "bypass_permissions"},
		{"--model", "gpt-5.5"},
		{"-c", `model_provider="shengsuanyun"`},
		{"-c", `openai_base_url="https://router.example.com/api/v1"`},
		{"thread-resume", "--image", "/tmp/a.png", "--image", "/tmp/b.jpg", "--json", "-"},
	} {
		if !containsSequence(args, want) {
			t.Fatalf("args missing sequence %v in %v", want, args)
		}
	}
}

func TestSessionAccessorsImagesAndClose(t *testing.T) {
	ts, err := newTraexSession(context.Background(), "traex", []string{"--profile", "dev"}, t.TempDir(), "model-a", "high", "plan", "thread-1", "https://api.example.com", []string{"A=B"}, "provider-a")
	if err != nil {
		t.Fatalf("newTraexSession: %v", err)
	}
	defer ts.Close()

	if !ts.Alive() || ts.CurrentSessionID() != "thread-1" || ts.GetModel() != "model-a" || ts.GetReasoningEffort() != "high" {
		t.Fatalf("unexpected session state: alive=%v id=%q model=%q effort=%q", ts.Alive(), ts.CurrentSessionID(), ts.GetModel(), ts.GetReasoningEffort())
	}
	if ts.GetWorkDir() == "" || ts.Events() == nil {
		t.Fatalf("missing workdir/events")
	}
	if err := ts.RespondPermission("req", core.PermissionResult{Behavior: "allow"}); err != nil {
		t.Fatalf("RespondPermission() = %v", err)
	}

	for mime, want := range map[string]string{
		"image/jpeg": ".jpg",
		"image/gif":  ".gif",
		"image/webp": ".webp",
		"image/png":  ".png",
		"text/plain": ".png",
	} {
		if got := traexImageExt(mime); got != want {
			t.Fatalf("traexImageExt(%q) = %q, want %q", mime, got, want)
		}
	}

	prompt, paths, err := ts.stageImages("", []core.ImageAttachment{{MimeType: "image/jpeg", Data: []byte("jpeg")}})
	if err != nil {
		t.Fatalf("stageImages: %v", err)
	}
	if prompt != "Please analyze the attached image(s)." || len(paths) != 1 || filepath.Ext(paths[0]) != ".jpg" {
		t.Fatalf("stageImages prompt=%q paths=%v", prompt, paths)
	}
	if data, err := os.ReadFile(paths[0]); err != nil || string(data) != "jpeg" {
		t.Fatalf("staged image data = %q err=%v", data, err)
	}

	usage := &core.ContextUsage{UsedTokens: 10, ContextWindow: 100}
	ts.contextUsage = usage
	cloned := ts.GetContextUsage()
	cloned.UsedTokens = 99
	if ts.GetContextUsage().UsedTokens != 10 {
		t.Fatal("GetContextUsage returned mutable internal pointer")
	}

	if err := ts.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}
	if ts.Alive() {
		t.Fatal("Close() left session alive")
	}
	if err := ts.Send("after close", nil, nil); err == nil || !strings.Contains(err.Error(), "session is closed") {
		t.Fatalf("Send() after close error = %v", err)
	}
}

func TestSendWithFakeCLIEmitsEventsArgsAndEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell CLI is POSIX-only")
	}

	binDir := t.TempDir()
	argsFile := filepath.Join(t.TempDir(), "args.txt")
	stdinFile := filepath.Join(t.TempDir(), "stdin.txt")
	envFile := filepath.Join(t.TempDir(), "env.txt")
	writeFakeTraexScript(t, binDir, `#!/bin/sh
printf '%s\n' "$@" > "$TRAE_FAKE_ARGS_FILE"
cat > "$TRAE_FAKE_STDIN_FILE"
{
  printf 'CONFIG_ENV=%s\n' "$CONFIG_ENV"
  printf 'OPENAI_API_KEY=%s\n' "$OPENAI_API_KEY"
  printf 'SESSION_ENV=%s\n' "$SESSION_ENV"
} > "$TRAE_FAKE_ENV_FILE"
printf '%s\n' '{"type":"thread.started","thread_id":"thread-from-cli"}'
printf '%s\n' '{"type":"turn.started"}'
printf '%s\n' '{"type":"item.completed","item":{"type":"agent_message","text":"fake answer"}}'
printf '%s\n' '{"type":"turn.completed"}'
`)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TRAE_FAKE_ARGS_FILE", argsFile)
	t.Setenv("TRAE_FAKE_STDIN_FILE", stdinFile)
	t.Setenv("TRAE_FAKE_ENV_FILE", envFile)

	got, err := New(map[string]any{
		"work_dir": t.TempDir(),
		"model":    "fallback-model",
		"cmd":      "traex --profile dev",
		"env":      map[string]string{"CONFIG_ENV": "one"},
	})
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	agent := got.(*Agent)
	agent.SetProviders([]core.ProviderConfig{{
		Name:   "p1",
		APIKey: "provider-key",
		Model:  "provider-model",
	}})
	if !agent.SetActiveProvider("p1") {
		t.Fatal("SetActiveProvider(p1) = false")
	}
	agent.SetSessionEnv([]string{"SESSION_ENV=two"})

	session, err := agent.StartSession(context.Background(), "")
	if err != nil {
		t.Fatalf("StartSession() = %v", err)
	}
	defer session.Close()

	if err := session.Send("hello fake", nil, nil); err != nil {
		t.Fatalf("Send() = %v", err)
	}
	text := requireEvent(t, session.Events(), core.EventText, argsFile, stdinFile, envFile)
	if text.Content != "fake answer" {
		t.Fatalf("text event content = %q", text.Content)
	}
	result := requireEvent(t, session.Events(), core.EventResult, argsFile, stdinFile, envFile)
	if !result.Done || result.SessionID != "thread-from-cli" {
		t.Fatalf("result event = %#v", result)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}

	argsData := waitForTraexFileContents(t, argsFile, "--profile", "dev", "exec", "--skip-git-repo-check", "--model", "provider-model", "--json", "--cd")
	args := strings.Fields(string(argsData))
	for _, want := range [][]string{
		{"--profile", "dev", "exec", "--skip-git-repo-check"},
		{"--model", "provider-model"},
		{"--json", "--cd"},
	} {
		if !containsSequence(args, want) {
			t.Fatalf("args missing %v in %v", want, args)
		}
	}
	stdinData := waitForTraexFileContents(t, stdinFile, "hello fake")
	if string(stdinData) != "hello fake" {
		t.Fatalf("stdin = %q", stdinData)
	}
	envData := waitForTraexFileContents(t, envFile, "CONFIG_ENV=one", "OPENAI_API_KEY=provider-key", "SESSION_ENV=two")
	for _, want := range []string{"CONFIG_ENV=one", "OPENAI_API_KEY=provider-key", "SESSION_ENV=two"} {
		if !strings.Contains(string(envData), want) {
			t.Fatalf("env missing %q in:\n%s", want, envData)
		}
	}
}

func TestSendWithFakeCLIErrorsAndReadJSONLines(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell CLI is POSIX-only")
	}

	binDir := t.TempDir()
	writeFakeTraexScript(t, binDir, `#!/bin/sh
printf 'not-json\n'
printf '%s\n' '{"type":"thread.started","thread_id":"thread-before-error"}'
printf 'fatal stderr\n' >&2
exit 7
`)

	ts, err := newTraexSession(context.Background(), filepath.Join(binDir, "traex"), nil, t.TempDir(), "", "", "default", "", "", nil, "")
	if err != nil {
		t.Fatalf("newTraexSession: %v", err)
	}
	defer ts.Close()

	if err := ts.Send("trigger", nil, nil); err != nil {
		t.Fatalf("Send() = %v", err)
	}
	evt := requireEvent(t, ts.Events(), core.EventError)
	if evt.Error == nil || evt.Error.Error() != "fatal stderr" {
		t.Fatalf("stderr event = %#v", evt)
	}

	if err := readJSONLines(strings.NewReader("one\n\ntwo"), func(line []byte) error {
		if string(line) == "two" {
			return errors.New("stop")
		}
		return nil
	}); err == nil || err.Error() != "stop" {
		t.Fatalf("readJSONLines handler error = %v", err)
	}
	if err := readJSONLines(errReader{}, func([]byte) error { return nil }); err == nil {
		t.Fatal("readJSONLines on errReader returned nil")
	}
}

func TestSessionFilesListHistoryDeleteAndFiltering(t *testing.T) {
	traeHome := t.TempDir()
	t.Setenv("TRAE_HOME", traeHome)
	workDir := t.TempDir()
	otherDir := t.TempDir()
	sessionDir := filepath.Join(traeHome, "cli", "sessions", "2026", "06", "26")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}

	sessionPath := filepath.Join(sessionDir, "rollout-session-a.jsonl")
	writeTraexJSONL(t, sessionPath,
		jsonLine(t, "session_meta", map[string]any{"id": "session-a", "cwd": workDir}),
		jsonLine(t, "response_item", map[string]any{
			"role": "user",
			"content": []map[string]any{
				{"type": "input_text", "text": "# AGENTS.md\nignore"},
				{"type": "input_text", "text": strings.Repeat("long ", 20)},
			},
		}),
		jsonLine(t, "response_item", map[string]any{
			"role":    "assistant",
			"content": []map[string]any{{"type": "output_text", "text": "assistant answer"}},
		}),
		"{not json",
	)
	writeTraexJSONL(t, filepath.Join(sessionDir, "rollout-session-b.jsonl"),
		jsonLine(t, "session_meta", map[string]any{"id": "session-b", "cwd": otherDir}),
	)

	sessions, err := listTraexSessions(workDir)
	if err != nil {
		t.Fatalf("listTraexSessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].ID != "session-a" {
		t.Fatalf("sessions = %#v", sessions)
	}
	if sessions[0].MessageCount != 2 {
		t.Fatalf("MessageCount = %d, want 2", sessions[0].MessageCount)
	}
	if len([]rune(sessions[0].Summary)) > 63 || !strings.HasSuffix(sessions[0].Summary, "...") {
		t.Fatalf("summary was not truncated: %q", sessions[0].Summary)
	}

	agent := &Agent{workDir: workDir}
	listed, err := agent.ListSessions(context.Background())
	if err != nil || len(listed) != 1 || listed[0].ID != "session-a" {
		t.Fatalf("Agent.ListSessions() = %#v, %v", listed, err)
	}
	history, err := agent.GetSessionHistory(context.Background(), "session-a", 1)
	if err != nil {
		t.Fatalf("GetSessionHistory: %v", err)
	}
	if len(history) != 1 || history[0].Role != "assistant" || history[0].Content != "assistant answer" {
		t.Fatalf("limited history = %#v", history)
	}
	if err := agent.DeleteSession(context.Background(), "missing"); err == nil {
		t.Fatal("DeleteSession(missing) returned nil")
	}
	if err := agent.DeleteSession(context.Background(), "session-a"); err != nil {
		t.Fatalf("DeleteSession(session-a): %v", err)
	}
	if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
		t.Fatalf("session file still exists or unexpected stat error: %v", err)
	}

	if got := parseTraexSessionFile(filepath.Join(sessionDir, "does-not-exist"), workDir); got != nil {
		t.Fatalf("parse missing file = %#v", got)
	}
	if !isUserPrompt("hello") || isUserPrompt("<system>") || isUserPrompt("#AGENTS.md") || isUserPrompt("   ") {
		t.Fatal("isUserPrompt classification mismatch")
	}
}

func TestSkillDirsAndMemoryDirs(t *testing.T) {
	traeHome := t.TempDir()
	t.Setenv("TRAE_HOME", traeHome)
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	workDir := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}

	dirs := walkUpTraexProjectSkillDirs(workDir, filepath.Dir(root))
	for _, want := range []string{
		filepath.Join(workDir, ".agents", "skills"),
		filepath.Join(workDir, ".trae", "skills"),
		filepath.Join(root, ".agents", "skills"),
		filepath.Join(root, ".trae", "skills"),
	} {
		if !containsString(dirs, filepath.Clean(want)) {
			t.Fatalf("walkUpTraexProjectSkillDirs missing %q in %v", want, dirs)
		}
	}
	if got := findTraexProjectRoot(workDir); got != root {
		t.Fatalf("findTraexProjectRoot() = %q, want %q", got, root)
	}
	if !sameTraexPath(workDir, filepath.Join(workDir, ".")) || sameTraexPath("", workDir) {
		t.Fatal("sameTraexPath mismatch")
	}
	unique := uniqueTraexSkillDirs([]string{"", "/b", "/a", "/a/."})
	if strings.Join(unique, ",") != "/a,/b" {
		t.Fatalf("uniqueTraexSkillDirs() = %v", unique)
	}

	agent := &Agent{workDir: workDir}
	skillDirs := agent.SkillDirs()
	for _, want := range []string{filepath.Join(traeHome, "skills"), filepath.Join(workDir, ".agents", "skills")} {
		if !containsString(skillDirs, filepath.Clean(want)) {
			t.Fatalf("SkillDirs missing %q in %v", want, skillDirs)
		}
	}
}

func TestContextUsageFromRollout(t *testing.T) {
	traeHome := t.TempDir()
	t.Setenv("TRAE_HOME", traeHome)
	sessionID := "ctx-session"
	sessionDir := filepath.Join(traeHome, "cli", "sessions", "2026", "06", "26")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	rolloutPath := filepath.Join(sessionDir, "rollout-"+sessionID+".jsonl")
	usageLine := jsonLine(t, "event_msg", map[string]any{
		"type": "token_count",
		"info": map[string]any{
			"last_token_usage": map[string]any{
				"total_tokens":            1234,
				"input_tokens":            1000,
				"cached_input_tokens":     111,
				"output_tokens":           234,
				"reasoning_output_tokens": 55,
			},
			"total_token_usage":    map[string]any{"total_tokens": 9999},
			"model_context_window": 200000,
		},
	})
	writeTraexJSONL(t, rolloutPath, `{"type":"event_msg","payload":{"type":"ignore"}}`, usageLine)

	usage, path, err := loadContextUsageFromRollout(sessionID, "")
	if err != nil {
		t.Fatalf("loadContextUsageFromRollout: %v", err)
	}
	if path != rolloutPath || usage.UsedTokens != 1234 || usage.CachedInputTokens != 111 || usage.ContextWindow != 200000 {
		t.Fatalf("usage=%#v path=%q", usage, path)
	}

	cachedUsage, cachedPath, err := loadContextUsageFromRollout("ignored", rolloutPath)
	if err != nil || cachedPath != rolloutPath || cachedUsage.TotalTokens != 1234 {
		t.Fatalf("cached usage=%#v path=%q err=%v", cachedUsage, cachedPath, err)
	}
	if got := parseContextUsageFromRolloutBytes([]byte("bad\n" + usageLine + "\n")); got == nil || got.TotalTokens != 1234 {
		t.Fatalf("parseContextUsageFromRolloutBytes() = %#v", got)
	}
	if got := contextUsageFromSnake(traexSnakeTokenUsage{InputTokens: 2, OutputTokens: 3}, 10); got == nil || got.UsedTokens != 5 {
		t.Fatalf("contextUsageFromSnake fallback = %#v", got)
	}
	if got := contextUsageFromSnake(traexSnakeTokenUsage{}, 10); got != nil {
		t.Fatalf("empty usage = %#v, want nil", got)
	}
	if got := currentContextTokens(0, 2, 3); got != 5 {
		t.Fatalf("currentContextTokens fallback = %d", got)
	}
	if cloned := cloneContextUsage(usage); cloned == usage || cloned.TotalTokens != usage.TotalTokens {
		t.Fatalf("cloneContextUsage = %#v original=%#v", cloned, usage)
	}
}

func TestParseTraexStdoutEvents(t *testing.T) {
	// Simulate the raw JSON lines from a real traex exec --json run
	events := []string{
		`{"type":"thread.started","thread_id":"thread-test-001"}`,
		`{"type":"turn.started"}`,
		`{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"hello world"}}`,
		`{"type":"turn.completed","usage":{"input_tokens":100,"cached_input_tokens":50,"output_tokens":20}}`,
	}

	for _, line := range events {
		var raw map[string]any
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			t.Fatalf("Invalid JSON: %s", line)
		}

		ts := &traexSession{
			events: make(chan core.Event, 1),
			ctx:    context.Background(),
		}
		// Just verify no panic on handleEvent
		ts.handleEvent(raw)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func containsSequence(values, want []string) bool {
	if len(want) == 0 {
		return true
	}
	for i := 0; i+len(want) <= len(values); i++ {
		ok := true
		for j := range want {
			if values[i+j] != want[j] {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func requireEvent(t *testing.T, ch <-chan core.Event, eventType core.EventType, diagnosticFiles ...string) core.Event {
	t.Helper()
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	select {
	case evt, ok := <-ch:
		if !ok {
			t.Fatalf("events channel closed waiting for %s; diagnostics=%s", eventType, readTraexDiagnostics(diagnosticFiles))
		}
		if evt.Type != eventType {
			t.Fatalf("event type = %s, want %s; event=%#v", evt.Type, eventType, evt)
		}
		return evt
	case <-timer.C:
		t.Fatalf("timed out waiting for %s event; diagnostics=%s", eventType, readTraexDiagnostics(diagnosticFiles))
		return core.Event{}
	}
}

func assertNoEvent(t *testing.T, ch <-chan core.Event) {
	t.Helper()
	select {
	case evt := <-ch:
		t.Fatalf("unexpected event: %#v", evt)
	case <-time.After(20 * time.Millisecond):
	}
}

func writeFakeTraexScript(t *testing.T, dir, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake traex shell script is POSIX-only")
	}
	if err := os.WriteFile(filepath.Join(dir, "traex"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake traex: %v", err)
	}
}

func waitForTraexFileContents(t *testing.T, path string, wants ...string) []byte {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	var data []byte
	var err error
	for time.Now().Before(deadline) {
		data, err = os.ReadFile(path)
		if err == nil {
			if missing := missingTraexSubstrings(string(data), wants); len(missing) == 0 {
				return data
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	t.Fatalf("%s missing %v:\n%s", path, missingTraexSubstrings(string(data), wants), string(data))
	return nil
}

func missingTraexSubstrings(text string, wants []string) []string {
	var missing []string
	for _, want := range wants {
		if !strings.Contains(text, want) {
			missing = append(missing, want)
		}
	}
	return missing
}

func readTraexDiagnostics(paths []string) string {
	var parts []string
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			parts = append(parts, path+"="+err.Error())
			continue
		}
		parts = append(parts, path+"="+string(data))
	}
	return strings.Join(parts, "; ")
}

func jsonLine(t *testing.T, typ string, payload any) string {
	t.Helper()
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	entry := map[string]any{
		"timestamp": time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano),
		"type":      typ,
		"payload":   json.RawMessage(payloadJSON),
	}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal entry: %v", err)
	}
	return string(data)
}

func writeTraexJSONL(t *testing.T, path string, lines ...string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	for _, line := range lines {
		if _, err := w.WriteString(line + "\n"); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("flush %s: %v", path, err)
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) {
	return 0, errors.New("read failed")
}
