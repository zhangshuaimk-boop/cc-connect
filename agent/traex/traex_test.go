package traex

import (
	"context"
	"encoding/json"
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
