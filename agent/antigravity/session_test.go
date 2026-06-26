package antigravity

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chenhg5/cc-connect/core"
)

func TestSlugify(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"cc-connect", "cc-connect"},
		{"Daily", "daily"},
		{"My Project", "my-project"},
		{"hello_world", "hello-world"},
		{"Test.123", "test-123"},
		{"---weird---", "weird"},
		{"", "project"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := slugify(tt.input)
			if got != tt.want {
				t.Errorf("slugify(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeMode(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"default", "default"},
		{"yolo", "yolo"},
		{"auto", "yolo"},
		{"force", "yolo"},
		{"plan", "plan"},
		{"sandbox", "plan"},
		{"invalid", "default"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeMode(tt.input)
			if got != tt.want {
				t.Errorf("normalizeMode(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSession_ContinueSessionTreatedAsFresh(t *testing.T) {
	s, err := newAntigravitySession(context.Background(), "echo", nil, "/tmp", "", "default", core.ContinueSession, nil, 0)
	if err != nil {
		t.Fatalf("newAntigravitySession: %v", err)
	}
	defer func() { _ = s.Close() }()

	if got := s.CurrentSessionID(); got != "" {
		t.Errorf("ContinueSession should be treated as fresh: chatID = %q, want empty", got)
	}
}

func TestBuildAntigravityArgs_PromptAtEnd(t *testing.T) {
	s, _ := newAntigravitySession(context.Background(), "echo", nil, "/tmp", "", "default", "", nil, 0)
	args := s.buildAntigravityArgs("sid-1", true, "plan", "What is 1+1?")
	if len(args) < 2 {
		t.Fatalf("args too short: %v", args)
	}
	if args[len(args)-2] != "-p" || args[len(args)-1] != "What is 1+1?" {
		t.Fatalf("expected prompt to be final '-p <prompt>', got: %v", args)
	}
	if !contains(args, "--sandbox") {
		t.Fatalf("expected --sandbox in args, got: %v", args)
	}
	if contains(args, "-m") || contains(args, "--model") {
		t.Fatalf("did not expect model flags in args, got: %v", args)
	}
}

func TestAgentStartSessionFakeCLI_MergesEnvAndStreamsOutput(t *testing.T) {
	tmp := t.TempDir()
	capturePath := filepath.Join(tmp, "capture.txt")
	cliPath := writeAntigravityFakeCLI(t, capturePath)

	agentInf, err := New(map[string]any{
		"work_dir": tmp,
		"model":    "configured-model",
		"mode":     "default",
		"cmd":      cliPath + " --wrapped",
		"env": map[string]any{
			"CONFIG_FLAG": "from-config",
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a := agentInf.(*Agent)
	if a.Name() != "antigravity" {
		t.Fatalf("Name() = %q, want antigravity", a.Name())
	}

	a.SetProviders([]core.ProviderConfig{{
		Name:   "google",
		APIKey: "provider-key",
		Model:  "provider-model",
		Env:    map[string]string{"PROVIDER_FLAG": "from-provider"},
	}})
	if !a.SetActiveProvider("google") {
		t.Fatal("SetActiveProvider(google) returned false")
	}
	a.SetSessionEnv([]string{"SESSION_FLAG=from-session"})

	session, err := a.StartSession(context.Background(), "resume-123")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	defer func() { _ = session.Close() }()

	if err := session.Send("hello", nil, nil); err != nil {
		t.Fatalf("Send: %v", err)
	}

	events := readAntigravityEventsUntilResult(t, session.Events(), capturePath)
	if !hasEventType(events, core.EventPermissionRequest) {
		t.Fatalf("expected permission request from terminal prompt, got %#v", events)
	}
	if !hasEventType(events, core.EventResult) {
		t.Fatalf("expected result event, got %#v", events)
	}

	_ = waitForFileContents(t, capturePath,
		"args=--wrapped --conversation resume-123 -p hello",
		"GEMINI_API_KEY=provider-key",
		"CONFIG_FLAG=from-config",
		"PROVIDER_FLAG=from-provider",
		"SESSION_FLAG=from-session",
	)
}

func TestAntigravitySessionDetectionAndListing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workDir := filepath.Join(t.TempDir(), "My Project")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("MkdirAll workDir: %v", err)
	}

	chatsDir := filepath.Join(home, ".gemini", "tmp", "custom-slug", "chats")
	if err := os.MkdirAll(chatsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll chatsDir: %v", err)
	}
	registryPath := filepath.Join(home, ".gemini", "projects.json")
	if err := os.WriteFile(registryPath, []byte(fmt.Sprintf(`{"projects":{%q:"custom-slug"}}`, filepath.Clean(workDir))), 0o644); err != nil {
		t.Fatalf("WriteFile registry: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	sessionPath := filepath.Join(chatsDir, "session-1.jsonl")
	sessionData := strings.Join([]string{
		fmt.Sprintf(`{"sessionId":"sid-1","projectHash":"custom-slug","startTime":%q,"lastUpdated":%q}`, now.Add(-time.Minute).Format(time.RFC3339), now.Format(time.RFC3339)),
		`{"type":"user","content":[{"text":"first user line\nsecond line"}]}`,
		`{"type":"assistant","content":[{"text":"reply"}]}`,
		fmt.Sprintf(`{"$set":{"lastUpdated":%q}}`, now.Add(time.Minute).Format(time.RFC3339)),
	}, "\n")
	if err := os.WriteFile(sessionPath, []byte(sessionData), 0o644); err != nil {
		t.Fatalf("WriteFile session: %v", err)
	}
	if err := os.WriteFile(filepath.Join(chatsDir, "subagent.jsonl"), []byte(`{"sessionId":"skip","kind":"subagent"}`+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile subagent: %v", err)
	}
	if err := os.WriteFile(filepath.Join(chatsDir, "nouser.jsonl"), []byte(`{"sessionId":"skip2"}`+"\n"+`{"type":"assistant","content":[{"text":"reply"}]}`), 0o644); err != nil {
		t.Fatalf("WriteFile nouser: %v", err)
	}

	a := &Agent{workDir: workDir}
	sessions, err := a.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("ListSessions() len = %d, want 1: %#v", len(sessions), sessions)
	}
	if got := sessions[0]; got.ID != "sid-1" || got.Summary != "first user line" || got.MessageCount != 2 {
		t.Fatalf("session info = %#v, want sid-1 summary and 2 messages", got)
	}

	as, err := newAntigravitySession(context.Background(), "agy", nil, workDir, "", "default", "", nil, 0)
	if err != nil {
		t.Fatalf("newAntigravitySession: %v", err)
	}
	defer func() { _ = as.Close() }()
	if got := as.detectNewSessionID(map[string]bool{}, now); got != "sid-1" {
		t.Fatalf("detectNewSessionID() = %q, want sid-1", got)
	}

	if err := a.DeleteSession(context.Background(), "sid-1"); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
		t.Fatalf("session file still exists after DeleteSession: %v", err)
	}
	if err := a.DeleteSession(context.Background(), "missing"); err == nil {
		t.Fatal("DeleteSession(missing) returned nil, want error")
	}
}

func TestUsesInteractivePermission(t *testing.T) {
	if !usesInteractivePermission("default") {
		t.Fatal("default mode should use interactive permission stdin")
	}
	if usesInteractivePermission("yolo") {
		t.Fatal("yolo mode should not use interactive permission stdin")
	}
	if usesInteractivePermission("plan") {
		t.Fatal("plan mode should not use interactive permission stdin")
	}
}

func TestRespondPermission_WritesTerminalAnswer(t *testing.T) {
	s, err := newAntigravitySession(context.Background(), "echo", nil, "/tmp", "", "default", "", nil, 0)
	if err != nil {
		t.Fatalf("newAntigravitySession: %v", err)
	}
	defer func() { _ = s.Close() }()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	defer func() { _ = r.Close() }()
	defer func() { _ = w.Close() }()
	s.stdin = w

	s.permReqID.Store("req")
	if err := s.RespondPermission("req", core.PermissionResult{Behavior: "allow"}); err != nil {
		t.Fatalf("RespondPermission allow: %v", err)
	}
	buf := make([]byte, 8)
	n, err := r.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("read allow response: %v", err)
	}
	if got := string(buf[:n]); got != "y\n" {
		t.Fatalf("allow response = %q, want %q", got, "y\n")
	}

	s.permReqID.Store("req")
	if err := s.RespondPermission("req", core.PermissionResult{Behavior: "deny"}); err != nil {
		t.Fatalf("RespondPermission deny: %v", err)
	}
	n, err = r.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("read deny response: %v", err)
	}
	if got := string(buf[:n]); got != "n\n" {
		t.Fatalf("deny response = %q, want %q", got, "n\n")
	}
}

func TestExtractPermissionPrompt(t *testing.T) {
	text := "Tool wants to run command. Allow this action? (y/N)"
	got, ok := extractPermissionPrompt(text)
	if !ok {
		t.Fatalf("expected permission prompt to be detected")
	}
	if got == "" {
		t.Fatalf("detected prompt should not be empty")
	}
}

func TestExtractPermissionPrompt_SplitChunksDetectedInWindow(t *testing.T) {
	part1 := "Tool wants to run command. Allow this"
	part2 := " action? (y/N)"
	got, ok := extractPermissionPrompt(part1 + part2)
	if !ok || got == "" {
		t.Fatalf("expected split prompt to be detected, got ok=%v prompt=%q", ok, got)
	}
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if strings.TrimSpace(x) == want {
			return true
		}
	}
	return false
}

func writeAntigravityFakeCLI(t *testing.T, capturePath string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agy")
	script := fmt.Sprintf(`#!/bin/sh
tmp=%q.$$
trap 'rm -f "$tmp"' EXIT
{
  printf 'args=%%s\n' "$*"
  printf 'GEMINI_API_KEY=%%s\n' "$GEMINI_API_KEY"
  printf 'CONFIG_FLAG=%%s\n' "$CONFIG_FLAG"
  printf 'PROVIDER_FLAG=%%s\n' "$PROVIDER_FLAG"
  printf 'SESSION_FLAG=%%s\n' "$SESSION_FLAG"
} > "$tmp"
mv "$tmp" %q
printf 'Allow terminal command? (y/N)\n'
printf 'agent output\n'
`, capturePath, capturePath)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile fake CLI: %v", err)
	}
	return path
}

func readAntigravityEventsUntilResult(t *testing.T, ch <-chan core.Event, capturePath string) []core.Event {
	t.Helper()
	var events []core.Event
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	for {
		select {
		case evt, ok := <-ch:
			if !ok {
				t.Fatalf("events channel closed before result, events=%#v, capture=%s", events, readFileForFailure(capturePath))
			}
			events = append(events, evt)
			if evt.Type == core.EventResult {
				return events
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for result, events=%#v, capture=%s", events, readFileForFailure(capturePath))
		}
	}
}

func hasEventType(events []core.Event, typ core.EventType) bool {
	for _, evt := range events {
		if evt.Type == typ {
			return true
		}
	}
	return false
}

func waitForFileContents(t *testing.T, path string, wants ...string) string {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	var data []byte
	var err error
	for time.Now().Before(deadline) {
		data, err = os.ReadFile(path)
		if err == nil {
			text := string(data)
			missing := missingSubstrings(text, wants)
			if len(missing) == 0 {
				return text
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	t.Fatalf("capture missing %v:\n%s", missingSubstrings(string(data), wants), string(data))
	return ""
}

func missingSubstrings(text string, wants []string) []string {
	var missing []string
	for _, want := range wants {
		if !strings.Contains(text, want) {
			missing = append(missing, want)
		}
	}
	return missing
}

func readFileForFailure(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return err.Error()
	}
	return string(data)
}
