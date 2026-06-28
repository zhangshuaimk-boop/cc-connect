package cursor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chenhg5/cc-connect/core"
)

func TestAgentMatrixAttributesModesAndProviders(t *testing.T) {
	a := &Agent{workDir: "/workspace", model: "base-model", mode: "default", cmd: "agent", activeIdx: -1}

	if a.Name() != "cursor" || a.CLIBinaryName() != "agent" || a.CLIDisplayName() != "Cursor Agent" {
		t.Fatalf("unexpected identity: name=%q cli=%q display=%q", a.Name(), a.CLIBinaryName(), a.CLIDisplayName())
	}
	if a.CompressCommand() != "" {
		t.Fatalf("CompressCommand() = %q, want empty", a.CompressCommand())
	}

	a.SetWorkDir("/next")
	if got := a.GetWorkDir(); got != "/next" {
		t.Fatalf("GetWorkDir() = %q, want /next", got)
	}
	a.SetMode("YOLO")
	if got := a.GetMode(); got != "force" {
		t.Fatalf("GetMode() = %q, want force", got)
	}
	a.SetMode("ask")
	if got := a.GetMode(); got != "ask" {
		t.Fatalf("GetMode() = %q, want ask", got)
	}
	if modes := a.PermissionModes(); len(modes) != 4 || modes[0].Key != "default" || modes[1].Key != "force" {
		t.Fatalf("PermissionModes() = %#v", modes)
	}

	a.SetModel("local-model")
	a.SetProviders([]core.ProviderConfig{
		{Name: "one", APIKey: "key-one", Model: "provider-model", Env: map[string]string{"CURSOR_BASE_URL": "https://api.example"}},
		{Name: "two", Model: "other-model"},
	})
	if got := a.GetModel(); got != "local-model" {
		t.Fatalf("GetModel() without active provider = %q, want local-model", got)
	}
	if !a.SetActiveProvider("one") {
		t.Fatal("SetActiveProvider(one) returned false")
	}
	if got := a.GetModel(); got != "provider-model" {
		t.Fatalf("GetModel() with active provider = %q, want provider-model", got)
	}
	active := a.GetActiveProvider()
	if active == nil || active.Name != "one" {
		t.Fatalf("GetActiveProvider() = %#v", active)
	}
	active.Name = "mutated"
	if got := a.GetActiveProvider().Name; got != "one" {
		t.Fatalf("GetActiveProvider returned mutable internal pointer; got %q", got)
	}
	if got := a.ListProviders(); len(got) != 2 || got[0].Name != "one" {
		t.Fatalf("ListProviders() = %#v", got)
	}
	if a.SetActiveProvider("missing") {
		t.Fatal("SetActiveProvider(missing) returned true")
	}
	if !a.SetActiveProvider("") || a.GetActiveProvider() != nil {
		t.Fatalf("clearing active provider failed: %#v", a.GetActiveProvider())
	}
}

func TestAgentMatrixStartSessionCapturesConfigState(t *testing.T) {
	a := &Agent{
		workDir:      "/workspace",
		model:        "base-model",
		mode:         "plan",
		cmd:          "agent",
		cliExtraArgs: []string{"--profile", "test"},
		configEnv:    []string{"STATIC_ENV=1"},
		providers: []core.ProviderConfig{
			{Name: "p1", APIKey: "api-key", Model: "provider-model", Env: map[string]string{"EXTRA_ENV": "provider"}},
		},
		activeIdx: 0,
	}
	a.SetSessionEnv([]string{"TURN_ENV=2"})

	sess, err := a.StartSession(context.Background(), "resume-123")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	defer sess.Close()

	cs := sess.(*cursorSession)
	if cs.workDir != "/workspace" || cs.model != "provider-model" || cs.mode != "plan" {
		t.Fatalf("captured session state workDir=%q model=%q mode=%q", cs.workDir, cs.model, cs.mode)
	}
	if got := cs.CurrentSessionID(); got != "resume-123" {
		t.Fatalf("CurrentSessionID() = %q, want resume-123", got)
	}
	for _, want := range []string{"STATIC_ENV=1", "CURSOR_API_KEY=api-key", "EXTRA_ENV=provider", "TURN_ENV=2"} {
		if !stringSliceContains(cs.extraEnv, want) {
			t.Fatalf("extraEnv missing %q: %#v", want, cs.extraEnv)
		}
	}
	if !stringSliceContains(cs.extraArgs, "--profile") || !stringSliceContains(cs.extraArgs, "test") {
		t.Fatalf("extraArgs = %#v, want profile args", cs.extraArgs)
	}
}

func TestCursorSessionHandleEventMatrix(t *testing.T) {
	cs := newTestSession("default")
	defer cs.cancel()

	cs.handleEvent(map[string]any{"type": "system", "session_id": "sid-1", "model": "claude"})
	cs.handleEvent(map[string]any{"type": "thinking", "subtype": "delta", "text": "think"})
	cs.handleEvent(map[string]any{"type": "thinking", "subtype": "done"})
	cs.handleEvent(map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"content": []any{map[string]any{"type": "text", "text": "answer"}},
		},
	})
	cs.handleEvent(map[string]any{
		"type":    "tool_call",
		"subtype": "started",
		"tool_call": map[string]any{
			"shellToolCall": map[string]any{"args": map[string]any{"command": "ls -la"}},
		},
	})
	cs.handleEvent(map[string]any{"type": "result", "result": "done", "session_id": "sid-1"})

	got := drainCursorEvents(cs)
	if len(got) != 5 {
		t.Fatalf("got %d events, want 5: %#v", len(got), got)
	}
	assertCursorEvent(t, got[0], core.EventText, "", "sid-1", "claude")
	assertCursorEvent(t, got[1], core.EventThinking, "think", "", "")
	assertCursorEvent(t, got[2], core.EventText, "answer", "", "")
	assertCursorEvent(t, got[3], core.EventToolUse, "", "", "Bash")
	if got[3].ToolInput != "ls -la" {
		t.Fatalf("tool input = %q, want ls -la", got[3].ToolInput)
	}
	assertCursorEvent(t, got[4], core.EventResult, "done", "sid-1", "")
	if !got[4].Done {
		t.Fatal("result event Done = false, want true")
	}
}

func TestCursorSessionSendUsesArgsEnvFilesAndReadsStream(t *testing.T) {
	tmp := t.TempDir()
	argsPath := filepath.Join(tmp, "args.txt")
	envPath := filepath.Join(tmp, "env.txt")
	fakeCLI := filepath.Join(tmp, "agent")
	script := fmt.Sprintf(`#!/bin/sh
args_tmp=%q.$$
env_tmp=%q.$$
trap 'rm -f "$args_tmp" "$env_tmp"' EXIT
printf '%%s\n' "$@" > "$args_tmp"
mv "$args_tmp" %q
printf '%%s\n' "$CURSOR_TEST_ENV:$TURN_ENV" > "$env_tmp"
mv "$env_tmp" %q
printf '%%s\n' '{"type":"system","session_id":"sid-from-cli","model":"fake-model"}'
printf '%%s\n' '{"type":"assistant","message":{"content":[{"type":"text","text":"hello from cli"}]}}'
printf '%%s\n' '{"type":"result","result":"final text","session_id":"sid-from-cli"}'
`, argsPath, envPath, argsPath, envPath)
	if err := os.WriteFile(fakeCLI, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake cli: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	sess, err := newCursorSession(ctx, fakeCLI, []string{"--profile", "unit"}, tmp, "model-x", "force", "", []string{
		"CURSOR_TEST_ENV=static",
		"TURN_ENV=session",
	})
	if err != nil {
		t.Fatalf("newCursorSession: %v", err)
	}
	defer sess.Close()

	files := []core.FileAttachment{{FileName: "note.txt", Data: []byte("attached")}}
	if err := sess.Send("prompt text", nil, files); err != nil {
		t.Fatalf("Send: %v", err)
	}

	var events []core.Event
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	for len(events) < 3 {
		select {
		case ev, ok := <-sess.Events():
			if !ok {
				t.Fatalf("events channel closed waiting for stream, got %#v, args=%s, env=%s",
					events, readCursorFileForFailure(argsPath), readCursorFileForFailure(envPath))
			}
			events = append(events, ev)
		case <-timer.C:
			t.Fatalf("timeout waiting for events, got %#v, args=%s, env=%s",
				events, readCursorFileForFailure(argsPath), readCursorFileForFailure(envPath))
		}
	}
	if events[0].SessionID != "sid-from-cli" || events[0].ToolName != "fake-model" {
		t.Fatalf("system event = %#v", events[0])
	}
	if events[1].Type != core.EventText || events[1].Content != "hello from cli" {
		t.Fatalf("text event = %#v", events[1])
	}
	if events[2].Type != core.EventResult || events[2].Content != "final text" || !events[2].Done {
		t.Fatalf("result event = %#v", events[2])
	}

	argsBytes := waitForCursorFileContents(t, argsPath,
		"--profile", "unit", "--print", "--output-format", "stream-json", "--force", "--model", "model-x", "--workspace", tmp, "prompt text",
		filepath.Join(tmp, ".cc-connect", "attachments", "note.txt"),
	)
	args := string(argsBytes)
	for _, want := range []string{"--profile", "unit", "--print", "--output-format", "stream-json", "--force", "--model", "model-x", "--workspace", tmp, "prompt text"} {
		if !strings.Contains(args, want) {
			t.Fatalf("args missing %q:\n%s", want, args)
		}
	}
	if !strings.Contains(args, filepath.Join(tmp, ".cc-connect", "attachments", "note.txt")) {
		t.Fatalf("prompt args did not include attachment path:\n%s", args)
	}
	envBytes := waitForCursorFileContents(t, envPath, "static:session")
	if strings.TrimSpace(string(envBytes)) != "static:session" {
		t.Fatalf("merged env = %q, want static:session", strings.TrimSpace(string(envBytes)))
	}
}

func TestCursorSessionErrorAndHelpers(t *testing.T) {
	cs := newTestSession("default")
	cs.alive.Store(false)
	if err := cs.Send("after close", nil, nil); err == nil || !strings.Contains(err.Error(), "session is closed") {
		t.Fatalf("Send on closed session error = %v", err)
	}

	for _, tc := range []struct {
		name string
		raw  map[string]any
		tool string
		in   string
	}{
		{"read", map[string]any{"readToolCall": map[string]any{"args": map[string]any{"path": "/tmp/a"}}}, "Read", "/tmp/a"},
		{"edit filePath", map[string]any{"editToolCall": map[string]any{"args": map[string]any{"filePath": "/tmp/b"}}}, "Edit", "/tmp/b"},
		{"grep", map[string]any{"grepToolCall": map[string]any{"args": map[string]any{"pattern": "TODO"}}}, "Grep", "TODO"},
		{"generic", map[string]any{"description": "custom tool"}, "Tool", "custom tool"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tool, input := extractToolInfo(tc.raw)
			if tool != tc.tool || input != tc.in {
				t.Fatalf("extractToolInfo() = %q/%q, want %q/%q", tool, input, tc.tool, tc.in)
			}
		})
	}

	if got := truncateStr("abcdef", 3); got != "abc..." {
		t.Fatalf("truncateStr() = %q, want abc...", got)
	}
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func waitForCursorFileContents(t *testing.T, path string, wants ...string) []byte {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	var data []byte
	var err error
	for time.Now().Before(deadline) {
		data, err = os.ReadFile(path)
		if err == nil {
			if missing := missingCursorSubstrings(string(data), wants); len(missing) == 0 {
				return data
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	t.Fatalf("%s missing %v:\n%s", path, missingCursorSubstrings(string(data), wants), string(data))
	return nil
}

func missingCursorSubstrings(text string, wants []string) []string {
	var missing []string
	for _, want := range wants {
		if !strings.Contains(text, want) {
			missing = append(missing, want)
		}
	}
	return missing
}

func readCursorFileForFailure(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return err.Error()
	}
	return string(data)
}

func drainCursorEvents(cs *cursorSession) []core.Event {
	var events []core.Event
	for {
		select {
		case ev := <-cs.events:
			events = append(events, ev)
		default:
			return events
		}
	}
}

func assertCursorEvent(t *testing.T, got core.Event, wantType core.EventType, wantContent, wantSessionID, wantTool string) {
	t.Helper()
	if got.Type != wantType || got.Content != wantContent || got.SessionID != wantSessionID || got.ToolName != wantTool {
		t.Fatalf("event = %#v, want type=%s content=%q session=%q tool=%q", got, wantType, wantContent, wantSessionID, wantTool)
	}
}
