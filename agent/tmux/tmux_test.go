package tmux

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chenhg5/cc-connect/core"
)

func TestExtractNew(t *testing.T) {
	tests := []struct {
		name     string
		baseline string
		current  string
		want     string
	}{
		{
			name:     "no change",
			baseline: "foo\nbar",
			current:  "foo\nbar",
			want:     "",
		},
		{
			name:     "empty baseline",
			baseline: "",
			current:  "hello",
			want:     "hello",
		},
		{
			name:     "content grew (fast path)",
			baseline: "foo\nbar",
			current:  "foo\nbar\nbaz",
			want:     "baz",
		},
		{
			name:     "new line after prompt",
			baseline: "user@host:~$ ",
			current:  "user@host:~$ ls\nfile1\nfile2\nuser@host:~$ ",
			want:     "ls\nfile1\nfile2\nuser@host:~$ ",
		},
		{
			name:     "anchor overlap",
			baseline: "line1\nline2\nline3\nline4\nline5",
			current:  "line3\nline4\nline5\nnew1\nnew2",
			want:     "new1\nnew2",
		},
		{
			name:     "fully scrolled - return all current",
			baseline: "old1\nold2\nold3",
			current:  "new1\nnew2\nnew3",
			want:     "new1\nnew2\nnew3",
		},
		{
			name:     "TUI redrawn - shared frame, response replaces prompt",
			baseline: "╭─ Claude ─╮\n\n>",
			current:  "╭─ Claude ─╮\n\nThe answer is 42.\n\n>",
			want:     "The answer is 42.",
		},
		{
			name:     "TUI redrawn - multi-line response",
			baseline: "header\n\n>",
			current:  "header\n\nLine one.\nLine two.\n\n>",
			want:     "Line one.\nLine two.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractNew(tt.baseline, tt.current)
			if got != tt.want {
				t.Errorf("extractNew() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeCapture(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "strip trailing spaces per line",
			raw:  "hello   \nworld   \n",
			want: "hello\nworld",
		},
		{
			name: "strip ANSI color codes",
			raw:  "\x1b[32mgreen\x1b[0m normal",
			want: "green normal",
		},
		{
			name: "strip OSC sequence",
			raw:  "\x1b]0;title\x07prompt$ ",
			want: "prompt$",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeCapture(tt.raw)
			if got != tt.want {
				t.Errorf("normalizeCapture() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewAgentValidation(t *testing.T) {
	// Missing session name should fail
	_, err := New(map[string]any{})
	if err == nil {
		t.Error("expected error when session is empty")
	}

	// With session name but tmux not in PATH - may fail on systems without tmux,
	// so we just verify the session check happens before the tmux PATH check.
}

// TestResolveTargetUniquePerWorkDir verifies that two workDirs sharing the same
// basename but different parent paths never map to the same tmux window target.
func TestResolveTargetUniquePerWorkDir(t *testing.T) {
	a := &Agent{sessionName: "mywork", pane: "0"}

	target1, win1 := a.resolveTarget("mywork", "0", "/repo/a/app")
	target2, win2 := a.resolveTarget("mywork", "0", "/repo/b/app")

	if target1 == target2 {
		t.Errorf("resolveTarget: collision — /repo/a/app and /repo/b/app both produced %q", target1)
	}
	if win1 == win2 {
		t.Errorf("uniqueWindowName: collision — /repo/a/app and /repo/b/app both produced %q", win1)
	}
}

// TestResolveTargetStable verifies that the same workDir always yields the same target.
func TestResolveTargetStable(t *testing.T) {
	a := &Agent{sessionName: "mywork", pane: "0"}

	t1, w1 := a.resolveTarget("mywork", "0", "/repo/a/app")
	t2, w2 := a.resolveTarget("mywork", "0", "/repo/a/app")

	if t1 != t2 || w1 != w2 {
		t.Errorf("resolveTarget not deterministic: got %q/%q then %q/%q", t1, w1, t2, w2)
	}
}

// TestNewTmuxSessionWorkDir verifies that the workDir is stored in the session so
// that file attachments are saved relative to the workspace, not to ".".
func TestNewTmuxSessionWorkDir(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s, err := newTmuxSession(ctx, "sess:win", "sid1", "", 200*time.Millisecond, false, nil, "/tmp/workspace")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if s.workDir != "/tmp/workspace" {
		t.Errorf("workDir = %q, want /tmp/workspace", s.workDir)
	}
}

func TestAgentMatrixBasicStateAndWorkspaceOptions(t *testing.T) {
	a := &Agent{
		sessionName:      "work",
		pane:             "1",
		workDir:          "/workspace",
		autoCreate:       true,
		shell:            "/bin/zsh",
		initCmd:          "claude",
		startupWaitMs:    123,
		promptPat:        `\$`,
		pollMs:           50,
		stripInputBlock:  true,
		stripPatterns:    []string{"drop"},
		windowPerSession: true,
	}

	if got := a.Name(); got != "tmux" {
		t.Fatalf("Name() = %q, want tmux", got)
	}
	a.SetWorkDir("/next")
	if got := a.GetWorkDir(); got != "/next" {
		t.Fatalf("GetWorkDir() = %q, want /next", got)
	}
	if got, err := a.ListSessions(context.Background()); err != nil || got != nil {
		t.Fatalf("ListSessions() = %#v, %v; want nil, nil", got, err)
	}
	if err := a.Stop(); err != nil {
		t.Fatalf("Stop(): %v", err)
	}

	opts := a.WorkspaceAgentOptions()
	checks := map[string]any{
		"session":            "work",
		"pane":               "1",
		"auto_create":        true,
		"shell":              "/bin/zsh",
		"init_command":       "claude",
		"startup_wait_ms":    123,
		"prompt_pattern":     `\$`,
		"poll_interval_ms":   50,
		"strip_input_block":  true,
		"window_per_session": true,
	}
	for key, want := range checks {
		if got := opts[key]; got != want {
			t.Fatalf("WorkspaceAgentOptions()[%q] = %#v, want %#v", key, got, want)
		}
	}
	if patterns, ok := opts["strip_patterns"].([]string); !ok || len(patterns) != 1 || patterns[0] != "drop" {
		t.Fatalf("strip_patterns option = %#v", opts["strip_patterns"])
	}
	if _, ok := opts["work_dir"]; ok {
		t.Fatalf("WorkspaceAgentOptions should not include work_dir: %#v", opts)
	}
}

func TestNewTmuxSessionValidationAndMethods(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if _, err := newTmuxSession(ctx, "sess:0", "", "[", time.Millisecond, true, nil, "."); err == nil {
		t.Fatal("expected invalid prompt_pattern error")
	}
	if _, err := newTmuxSession(ctx, "sess:0", "", "", time.Millisecond, true, []string{"["}, "."); err == nil {
		t.Fatal("expected invalid strip_pattern error")
	}

	s, err := newTmuxSession(ctx, "sess:0", "sid-1", `\$`, time.Millisecond, true, []string{`^drop`}, "/work")
	if err != nil {
		t.Fatalf("newTmuxSession: %v", err)
	}
	if !s.Alive() {
		t.Fatal("Alive() = false, want true")
	}
	if got := s.CurrentSessionID(); got != "sid-1" {
		t.Fatalf("CurrentSessionID() = %q, want sid-1", got)
	}
	if s.Events() == nil {
		t.Fatal("Events() returned nil")
	}
	if err := s.RespondPermission("req", core.PermissionResult{Behavior: "allow"}); err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("RespondPermission() error = %v", err)
	}
	s.safeSend(core.Event{Type: core.EventText, Content: "queued"})
	select {
	case ev := <-s.Events():
		if ev.Type != core.EventText || ev.Content != "queued" {
			t.Fatalf("safeSend event = %#v", ev)
		}
	default:
		t.Fatal("safeSend did not enqueue event")
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close(): %v", err)
	}
	if s.Alive() {
		t.Fatal("Alive() = true after Close")
	}
	if err := s.Send("closed", nil, nil); err == nil || !strings.Contains(err.Error(), "session closed") {
		t.Fatalf("Send after Close error = %v", err)
	}
}

func TestCleanTUIContentAndShellHelpers(t *testing.T) {
	s, err := newTmuxSession(context.Background(), "sess:0", "", "", time.Millisecond, true, []string{`^drop me`}, ".")
	if err != nil {
		t.Fatalf("newTmuxSession: %v", err)
	}
	defer s.Close()

	input := "keep\n────────────────\n❯ user input\n────────────────\ndrop me status\nanswer\n"
	if got := s.cleanTUIContent(input); got != "keep\n\nanswer" {
		t.Fatalf("cleanTUIContent() = %q, want keep\\n\\nanswer", got)
	}
	if got := shellQuote("a'b"); got != "'a'\\''b'" {
		t.Fatalf("shellQuote() = %q", got)
	}
	if got := sanitizeWindowName("a:b.c d"); got != "a-b-c-d" {
		t.Fatalf("sanitizeWindowName() = %q", got)
	}
	if got := sanitizeWindowName(""); got != "default" {
		t.Fatalf("sanitizeWindowName(empty) = %q", got)
	}
}

func TestExtractResponseFallsBackToPaneOnScrollbackError(t *testing.T) {
	s, err := newTmuxSession(context.Background(), "definitely-missing-session:0", "sid", "", time.Millisecond, true, nil, ".")
	if err != nil {
		t.Fatalf("newTmuxSession: %v", err)
	}
	defer s.Close()

	got := s.extractResponse()
	if got != "" {
		t.Fatalf("extractResponse() with missing tmux target = %q, want empty fallback", got)
	}
}

func TestSendClosedSessionBeforeTmuxCommands(t *testing.T) {
	s, err := newTmuxSession(context.Background(), "sess:0", "sid", "", time.Millisecond, false, nil, t.TempDir())
	if err != nil {
		t.Fatalf("newTmuxSession: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close(): %v", err)
	}
	if err := s.Send("prompt", nil, []core.FileAttachment{{FileName: filepath.Join("..", "x.txt"), Data: []byte("x")}}); err == nil {
		t.Fatal("expected closed session error")
	}
}

func TestTmuxCommandHelpersFailForMissingTarget(t *testing.T) {
	target := "cc-connect-missing-session:missing"
	if tmuxSessionExists("cc-connect-missing-session") {
		t.Skip("unexpected local tmux session exists")
	}
	if tmuxWindowExists(target) {
		t.Fatal("tmuxWindowExists returned true for missing target")
	}
	if _, err := capturePane(target); err == nil {
		t.Fatal("capturePane missing target returned nil error")
	}
	if _, err := captureScrollback(target); err == nil {
		t.Fatal("captureScrollback missing target returned nil error")
	}
	if err := sendKeys(target, "hello"); err == nil {
		t.Fatal("sendKeys missing target returned nil error")
	}
}

func TestCreateTmuxHelpersSurfaceCommandErrors(t *testing.T) {
	if err := createTmuxWindow("cc-connect-missing-session", "win", "."); err == nil {
		t.Fatal("createTmuxWindow missing session returned nil error")
	}
	if err := createTmuxSession("", "win", ".", ""); err == nil {
		t.Fatal("createTmuxSession empty name returned nil error")
	}
}
