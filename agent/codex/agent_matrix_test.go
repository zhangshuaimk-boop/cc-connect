package codex

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/chenhg5/cc-connect/core"
)

func TestAgentMatrix_BasicConfigProvidersAndSessionState(t *testing.T) {
	workDir := t.TempDir()
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)

	a := &Agent{
		workDir:         workDir,
		model:           "local-model",
		reasoningEffort: "low",
		mode:            "suggest",
		backend:         "exec",
		codexHome:       codexHome,
		cmd:             "codex",
		configEnv:       []string{"STATIC=1", "OPENAI_API_KEY=config-key"},
		activeIdx:       -1,
	}

	if got := a.Name(); got != "codex" {
		t.Fatalf("Name() = %q, want codex", got)
	}
	a.SetWorkDir(filepath.Join(workDir, "child"))
	if got := a.GetWorkDir(); got != filepath.Join(workDir, "child") {
		t.Fatalf("GetWorkDir() = %q", got)
	}
	a.SetModel("configured-model")
	if got := a.GetModel(); got != "configured-model" {
		t.Fatalf("GetModel() = %q, want configured-model", got)
	}
	a.SetReasoningEffort("med")
	if got := a.GetReasoningEffort(); got != "medium" {
		t.Fatalf("GetReasoningEffort() = %q, want medium", got)
	}
	a.SetMode("full_auto")
	if got := a.GetMode(); got != "full-auto" {
		t.Fatalf("GetMode() = %q, want full-auto", got)
	}
	if got := a.CompressCommand(); got != "" {
		t.Fatalf("CompressCommand() = %q, want empty", got)
	}
	if got := a.ProjectMemoryFile(); !strings.HasSuffix(got, filepath.Join("child", "AGENTS.md")) {
		t.Fatalf("ProjectMemoryFile() = %q", got)
	}
	if got := a.GlobalMemoryFile(); got != filepath.Join(codexHome, "AGENTS.md") {
		t.Fatalf("GlobalMemoryFile() = %q, want CODEX_HOME AGENTS.md", got)
	}
	if modes := a.PermissionModes(); len(modes) != 4 || modes[0].Key != "suggest" || modes[3].Key != "yolo" {
		t.Fatalf("PermissionModes() = %#v", modes)
	}

	providers := []core.ProviderConfig{
		{Name: "openai", APIKey: "key-a", BaseURL: "https://api-a.example/v1", Model: "provider-model", Env: map[string]string{"CUSTOM": "A"}},
		{Name: "router", APIKey: "key-b", BaseURL: "https://api-b.example/v1", Model: "router-model"},
	}
	a.SetProviders(providers)
	copied := a.ListProviders()
	copied[0].Name = "mutated"
	if got := a.ListProviders()[0].Name; got != "openai" {
		t.Fatalf("ListProviders() returned mutable backing slice, got first provider %q", got)
	}
	if a.GetActiveProvider() != nil {
		t.Fatal("GetActiveProvider() before SetActiveProvider = non-nil")
	}
	if ok := a.SetActiveProvider("missing"); ok {
		t.Fatal("SetActiveProvider(missing) = true, want false")
	}
	if ok := a.SetActiveProvider("openai"); !ok {
		t.Fatal("SetActiveProvider(openai) = false, want true")
	}
	if got := a.GetModel(); got != "provider-model" {
		t.Fatalf("GetModel() with active provider = %q, want provider-model", got)
	}
	if p := a.GetActiveProvider(); p == nil || p.Name != "openai" {
		t.Fatalf("GetActiveProvider() = %#v, want openai", p)
	}

	a.SetSessionEnv([]string{"OPENAI_API_KEY=session-key", "TURN=1"})
	sess, err := a.StartSession(context.Background(), "thread-existing")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	cs := sess.(*codexSession)
	defer cs.Close()
	if got := cs.CurrentSessionID(); got != "thread-existing" {
		t.Fatalf("CurrentSessionID() = %q, want thread-existing", got)
	}
	if got := cs.GetWorkDir(); got != filepath.Join(workDir, "child") {
		t.Fatalf("session GetWorkDir() = %q", got)
	}
	if got := cs.GetModel(); got != "provider-model" {
		t.Fatalf("session GetModel() = %q, want provider-model", got)
	}
	if got := cs.GetReasoningEffort(); got != "medium" {
		t.Fatalf("session GetReasoningEffort() = %q, want medium", got)
	}
	if !cs.Alive() {
		t.Fatal("new session Alive() = false")
	}
	env := envSliceToMap(cs.extraEnv)
	if got := env["STATIC"]; got != "1" {
		t.Fatalf("STATIC env = %q, want 1", got)
	}
	if got := env["OPENAI_BASE_URL"]; got != "https://api-a.example/v1" {
		t.Fatalf("OPENAI_BASE_URL = %q, want provider base URL", got)
	}
	if got := env["OPENAI_API_KEY"]; got != "session-key" {
		t.Fatalf("OPENAI_API_KEY = %q, want session override", got)
	}
	if got := env["TURN"]; got != "1" {
		t.Fatalf("TURN env = %q, want 1", got)
	}
	if got := env["CODEX_HOME"]; got != codexHome {
		t.Fatalf("CODEX_HOME = %q, want %q", got, codexHome)
	}
}

func TestAgentMatrix_ListHistoryAndDeleteSessions(t *testing.T) {
	workDir := t.TempDir()
	otherDir := t.TempDir()
	codexHome := t.TempDir()
	sessionsDir := filepath.Join(codexHome, "sessions", "2026", "06")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	absWorkDir, err := filepath.Abs(workDir)
	if err != nil {
		t.Fatalf("abs workdir: %v", err)
	}
	absOtherDir, err := filepath.Abs(otherDir)
	if err != nil {
		t.Fatalf("abs otherdir: %v", err)
	}

	sessionPath := filepath.Join(sessionsDir, "rollout-session-1.jsonl")
	writeCodexSessionJSONL(t, sessionPath, "session-1", absWorkDir, []codexHistoryLine{
		{Role: "user", Type: "input_text", Text: "# AGENTS.md\nskip me"},
		{Role: "user", Type: "input_text", Text: "first real prompt"},
		{Role: "assistant", Type: "output_text", Text: "assistant answer"},
		{Role: "user", Type: "input_text", Text: "second real prompt that is deliberately long enough to be truncated in the session list summary"},
	})
	otherPath := filepath.Join(sessionsDir, "rollout-session-2.jsonl")
	writeCodexSessionJSONL(t, otherPath, "session-2", absOtherDir, []codexHistoryLine{
		{Role: "user", Type: "input_text", Text: "other prompt"},
	})

	a := &Agent{workDir: workDir, codexHome: codexHome}
	sessions, err := a.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("ListSessions() len = %d, want 1: %#v", len(sessions), sessions)
	}
	if sessions[0].ID != "session-1" {
		t.Fatalf("session ID = %q, want session-1", sessions[0].ID)
	}
	if sessions[0].MessageCount != 4 {
		t.Fatalf("MessageCount = %d, want 4", sessions[0].MessageCount)
	}
	if !strings.HasPrefix(sessions[0].Summary, "second real prompt") || !strings.HasSuffix(sessions[0].Summary, "...") {
		t.Fatalf("Summary = %q, want truncated last real prompt", sessions[0].Summary)
	}

	history, err := a.GetSessionHistory(context.Background(), "session-1", 2)
	if err != nil {
		t.Fatalf("GetSessionHistory: %v", err)
	}
	want := []core.HistoryEntry{
		{Role: "assistant", Content: "assistant answer"},
		{Role: "user", Content: "second real prompt that is deliberately long enough to be truncated in the session list summary"},
	}
	if len(history) != len(want) {
		t.Fatalf("history len = %d, want %d: %#v", len(history), len(want), history)
	}
	for i := range want {
		if history[i].Role != want[i].Role || history[i].Content != want[i].Content {
			t.Fatalf("history[%d] = %#v, want role=%q content=%q", i, history[i], want[i].Role, want[i].Content)
		}
	}

	if err := a.DeleteSession(context.Background(), "session-1"); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
		t.Fatalf("session file after DeleteSession stat err = %v, want not exist", err)
	}
	if err := a.DeleteSession(context.Background(), "missing-session"); err == nil {
		t.Fatal("DeleteSession(missing) = nil, want error")
	}
}

func TestAgentMatrix_CodexEventParsingPaths(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cs := &codexSession{
		events: make(chan core.Event, 16),
		ctx:    ctx,
	}
	cs.alive.Store(true)

	cs.handleEvent(map[string]any{"type": "thread.started", "thread_id": "thread-1"})
	cs.handleEvent(map[string]any{"type": "turn.started"})
	cs.handleEvent(map[string]any{
		"type": "item.completed",
		"item": map[string]any{
			"type":    "reasoning",
			"summary": []any{map[string]any{"type": "summary_text", "text": "thinking"}},
		},
	})
	cs.handleEvent(map[string]any{
		"type": "item.completed",
		"item": map[string]any{
			"type":    "agent_message",
			"content": []any{map[string]any{"type": "output_text", "text": "draft answer"}},
		},
	})
	cs.handleEvent(map[string]any{
		"type": "item.started",
		"item": map[string]any{"type": "command_execution", "command": "go test ./agent/codex"},
	})
	cs.handleEvent(map[string]any{
		"type": "item.completed",
		"item": map[string]any{
			"type":               "command_execution",
			"command":            "go test ./agent/codex",
			"status":             "completed",
			"aggregated_output":  strings.Repeat("x", 550),
			"exit_code":          float64(0),
			"formatted_command":  "ignored",
			"formatted_output":   "ignored",
			"formatted_exitcode": "ignored",
		},
	})
	cs.handleEvent(map[string]any{
		"type": "item.completed",
		"item": map[string]any{
			"type":      "function_call",
			"name":      "apply_patch",
			"status":    "failed",
			"output":    "patch failed",
			"arguments": `{"file":"x"}`,
		},
	})
	cs.handleEvent(map[string]any{
		"type": "item.completed",
		"item": map[string]any{
			"type":    "agent_message",
			"content": []any{map[string]any{"type": "output_text", "text": "final answer"}},
		},
	})
	cs.handleEvent(map[string]any{
		"type": "item.completed",
		"item": map[string]any{
			"type":  "web_search",
			"query": "cc-connect",
		},
	})
	cs.handleEvent(map[string]any{"type": "turn.completed"})

	var events []core.Event
	for len(cs.events) > 0 {
		events = append(events, <-cs.events)
	}
	if got := cs.CurrentSessionID(); got != "thread-1" {
		t.Fatalf("CurrentSessionID() = %q, want thread-1", got)
	}
	assertEventSequence(t, events, []core.EventType{
		core.EventThinking,
		core.EventThinking,
		core.EventToolUse,
		core.EventToolResult,
		core.EventToolResult,
		core.EventToolUse,
		core.EventText,
		core.EventResult,
	})
	if events[0].Content != "thinking" {
		t.Fatalf("reasoning content = %q, want thinking", events[0].Content)
	}
	if events[1].Content != "draft answer" {
		t.Fatalf("pending message flushed as thinking = %q", events[1].Content)
	}
	if events[2].ToolName != "Bash" || events[2].ToolInput != "go test ./agent/codex" {
		t.Fatalf("command tool use = %#v", events[2])
	}
	if events[3].ToolName != "Bash" || events[3].ToolExitCode == nil || *events[3].ToolExitCode != 0 || events[3].ToolSuccess == nil || !*events[3].ToolSuccess {
		t.Fatalf("command result = %#v", events[3])
	}
	if !strings.HasSuffix(events[3].ToolResult, "...") {
		t.Fatalf("command result should be truncated, got len=%d", len(events[3].ToolResult))
	}
	if events[4].ToolName != "apply_patch" || events[4].ToolSuccess == nil || *events[4].ToolSuccess {
		t.Fatalf("function result = %#v", events[4])
	}
	if events[5].ToolName != "WebSearch" || !strings.Contains(events[5].ToolInput, "cc-connect") {
		t.Fatalf("web search tool use = %#v", events[5])
	}
	if events[6].Content != "final answer" {
		t.Fatalf("final text = %q, want pending answer", events[6].Content)
	}
	if events[7].SessionID != "thread-1" || !events[7].Done {
		t.Fatalf("result event = %#v", events[7])
	}
}

func TestAgentMatrix_CodexErrorAndHelperPaths(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cs := &codexSession{events: make(chan core.Event, 4), ctx: ctx}

	cs.handleEvent(map[string]any{
		"type":  "turn.failed",
		"error": map[string]any{"message": "quota exceeded"},
	})
	evt := <-cs.events
	if evt.Type != core.EventError || evt.Error == nil || !strings.Contains(evt.Error.Error(), "quota exceeded") {
		t.Fatalf("turn.failed event = %#v", evt)
	}
	cs.handleEvent(map[string]any{"type": "turn.failed"})
	evt = <-cs.events
	if evt.Type != core.EventError || evt.Error == nil || !strings.Contains(evt.Error.Error(), "no details") {
		t.Fatalf("turn.failed fallback event = %#v", evt)
	}

	if got := codexToolSuccess("completed", nil); !got {
		t.Fatal("codexToolSuccess(completed, nil) = false, want true")
	}
	code := 2
	if got := codexToolSuccess("completed", &code); got {
		t.Fatal("codexToolSuccess(completed, exit 2) = true, want false")
	}
	if got := codexExtractToolInput(map[string]any{"action": "search", "query": "agent"}); !strings.Contains(got, "agent") {
		t.Fatalf("codexExtractToolInput() = %q, want serialized input", got)
	}
	if got := codexImageExt("image/gif"); got != ".gif" {
		t.Fatalf("codexImageExt(gif) = %q", got)
	}
	if got := codexImageExt("image/webp"); got != ".webp" {
		t.Fatalf("codexImageExt(webp) = %q", got)
	}
}

type codexHistoryLine struct {
	Role string
	Type string
	Text string
}

func writeCodexSessionJSONL(t *testing.T, path, id, cwd string, lines []codexHistoryLine) {
	t.Helper()
	var out strings.Builder
	metaPayload := map[string]any{
		"id":         id,
		"cwd":        cwd,
		"source":     "exec",
		"originator": "codex_exec",
	}
	writeCodexJSONLRecord(t, &out, "2026-06-26T10:00:00Z", "session_meta", metaPayload)
	for i, line := range lines {
		payload := map[string]any{
			"role": line.Role,
			"content": []map[string]any{{
				"type": line.Type,
				"text": line.Text,
			}},
		}
		ts := time.Date(2026, 6, 26, 10, i+1, 0, 0, time.UTC).Format(time.RFC3339Nano)
		writeCodexJSONLRecord(t, &out, ts, "response_item", payload)
	}
	if err := os.WriteFile(path, []byte(out.String()), 0o644); err != nil {
		t.Fatalf("write session jsonl: %v", err)
	}
}

func writeCodexJSONLRecord(t *testing.T, out *strings.Builder, timestamp, recordType string, payload any) {
	t.Helper()
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	record := map[string]any{
		"timestamp": timestamp,
		"type":      recordType,
		"payload":   json.RawMessage(payloadJSON),
	}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal record: %v", err)
	}
	out.Write(data)
	out.WriteByte('\n')
}

func assertEventSequence(t *testing.T, events []core.Event, want []core.EventType) {
	t.Helper()
	got := make([]core.EventType, len(events))
	for i := range events {
		got[i] = events[i].Type
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %#v, want %#v; events=%#v", got, want, events)
	}
}
