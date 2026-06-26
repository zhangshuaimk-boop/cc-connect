package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chenhg5/cc-connect/core"
)

func TestSessionsListShowAndLoadMetadata(t *testing.T) {
	dataDir := t.TempDir()
	now := time.Date(2026, 6, 26, 10, 30, 0, 0, time.Local)
	writeSessionSnapshot(t, dataDir, "alpha", sessionFileData{
		Sessions: map[string]*sessionData{
			"s-old": {
				ID:        "s-old",
				Name:      "older",
				UpdatedAt: now.Add(-time.Hour),
				History: []core.HistoryEntry{
					{Role: "user", Content: "old prompt", Timestamp: now.Add(-time.Hour)},
				},
			},
			"s-new": {
				ID:        "s-new",
				Name:      "newer",
				UpdatedAt: now,
				History: []core.HistoryEntry{
					{Role: "user", Content: "first", Timestamp: now.Add(-2 * time.Minute)},
					{Role: "assistant", Content: "second", Timestamp: now.Add(-time.Minute)},
					{Role: "user", Content: "third", Timestamp: now},
				},
			},
		},
		UserSessions: map[string][]string{
			"feishu:chat-alpha:user-a": {"s-old", "s-new"},
		},
		UserMeta: map[string]*userMetaData{
			"feishu:chat-alpha:user-a": {UserName: "Alice", ChatName: "Release Room"},
		},
	})
	writeSessionSnapshot(t, dataDir, "beta", sessionFileData{
		Sessions: map[string]*sessionData{
			"s-beta": {
				ID:        "s-beta",
				Name:      "beta session",
				UpdatedAt: now.Add(-30 * time.Minute),
				History: []core.HistoryEntry{
					{Role: "user", Content: "beta prompt", Timestamp: now.Add(-30 * time.Minute)},
				},
			},
		},
		UserSessions: map[string][]string{
			"feishu:chat-beta:user-b": {"s-beta"},
		},
		UserMeta: map[string]*userMetaData{
			"feishu:chat-beta:user-b": {UserName: "Bob", ChatName: "Other Room"},
		},
	})
	if err := os.WriteFile(filepath.Join(dataDir, "sessions", "bad.json"), []byte("{not-json"), 0o644); err != nil {
		t.Fatalf("write corrupt snapshot: %v", err)
	}

	stdout, stderr := captureP8CommandOutput(t, func() {
		runSessions([]string{"--data-dir", dataDir, "list"})
	})
	if !strings.Contains(stderr, "Warning: cannot parse bad.json") {
		t.Fatalf("list stderr missing corrupt metadata warning:\n%s", stderr)
	}
	for _, want := range []string{"alpha", "beta", "Alice", "Bob", "Release Room", "Other Room"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("list stdout missing %q:\n%s", want, stdout)
		}
	}
	alphaIdx := strings.Index(stdout, "alpha")
	betaIdx := strings.Index(stdout, "beta")
	if alphaIdx < 0 || betaIdx < 0 || alphaIdx > betaIdx {
		t.Fatalf("sessions are not sorted by last activity descending:\n%s", stdout)
	}

	stdout, stderr = captureP8CommandOutput(t, func() {
		runSessions([]string{"--data-dir", dataDir, "show", "alpha:s-new", "-n", "2"})
	})
	if !strings.Contains(stderr, "Warning: cannot parse bad.json") {
		t.Fatalf("show stderr missing corrupt metadata warning:\n%s", stderr)
	}
	if !strings.Contains(stdout, "Session: alpha:s-new (newer)") || !strings.Contains(stdout, "Platform: feishu | User: Alice | Group: Release Room | Messages: 3") {
		t.Fatalf("show stdout missing header details:\n%s", stdout)
	}
	if strings.Contains(stdout, "first") || !strings.Contains(stdout, "second") || !strings.Contains(stdout, "third") {
		t.Fatalf("show -n did not limit to last two messages:\n%s", stdout)
	}
}

func TestSessionsEmptyAndCorruptOnly(t *testing.T) {
	dataDir := t.TempDir()

	stdout, stderr := captureP8CommandOutput(t, func() {
		runSessions([]string{"--data-dir", dataDir, "list"})
	})
	if stderr != "" {
		t.Fatalf("empty list stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, "No sessions found.") {
		t.Fatalf("empty list stdout = %q, want no sessions message", stdout)
	}

	if err := os.MkdirAll(filepath.Join(dataDir, "sessions"), 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "sessions", "bad.json"), []byte("{bad"), 0o644); err != nil {
		t.Fatalf("write bad snapshot: %v", err)
	}
	stdout, stderr = captureP8CommandOutput(t, func() {
		runSessions([]string{"--data-dir", dataDir, "list"})
	})
	if !strings.Contains(stderr, "Warning: cannot parse bad.json") {
		t.Fatalf("corrupt list stderr missing warning:\n%s", stderr)
	}
	if !strings.Contains(stdout, "No sessions found.") {
		t.Fatalf("corrupt-only list stdout = %q, want no sessions message", stdout)
	}
}

func TestSessionsUsageAndDisplayHelpers(t *testing.T) {
	stdout, stderr := captureP8CommandOutput(t, func() {
		runSessions([]string{"--help"})
	})
	if stderr != "" {
		t.Fatalf("sessions help stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, "Usage: cc-connect sessions") || !strings.Contains(stdout, "prune") {
		t.Fatalf("sessions help stdout missing usage:\n%s", stdout)
	}

	explicitDir := t.TempDir()
	if got := resolveDataDir(explicitDir); got != explicitDir {
		t.Fatalf("resolveDataDir explicit = %q, want %q", got, explicitDir)
	}
	if got := displayGroup(sessionRecord{}); got != "-" {
		t.Fatalf("displayGroup empty = %q, want -", got)
	}
	if got := displayGroup(sessionRecord{GroupUser: "chat-id"}); got != "chat-id" {
		t.Fatalf("displayGroup group user = %q, want chat-id", got)
	}
}

func TestSessionsShowIndexAndErrorPaths(t *testing.T) {
	dataDir := t.TempDir()
	now := time.Date(2026, 6, 26, 11, 0, 0, 0, time.Local)
	writeSessionSnapshot(t, dataDir, "alpha", sessionFileData{
		Sessions: map[string]*sessionData{
			"s1": {ID: "s1", Name: "first", UpdatedAt: now, History: []core.HistoryEntry{{Role: "user", Content: "hello", Timestamp: now}}},
		},
		UserSessions: map[string][]string{"feishu:chat:user": {"s1"}},
	})

	stdout, stderr := captureP8CommandOutput(t, func() {
		runSessions([]string{"--data-dir", dataDir, "show", "#1"})
	})
	if stderr != "" {
		t.Fatalf("show by index stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, "Session: alpha:s1") || !strings.Contains(stdout, "hello") {
		t.Fatalf("show by index stdout missing session:\n%s", stdout)
	}

	result := runSessionsCommandHelper(t, "--data-dir", dataDir, "show")
	if result.code == 0 || !strings.Contains(result.stderr, "session ID is required") {
		t.Fatalf("show without id result = %+v, want required id failure", result)
	}

	result = runSessionsCommandHelper(t, "--data-dir", dataDir, "show", "missing")
	if result.code == 0 || !strings.Contains(result.stderr, `session "missing" not found`) {
		t.Fatalf("show missing result = %+v, want not found failure", result)
	}

	result = runSessionsCommandHelper(t, "--data-dir", dataDir, "show", "alpha:s1", "-n", "bad")
	if result.code == 0 || !strings.Contains(result.stderr, "invalid -n value") {
		t.Fatalf("show bad limit result = %+v, want invalid -n failure", result)
	}
}

func TestSessionsPruneProjectAndEmptyMode(t *testing.T) {
	dataDir := t.TempDir()
	now := time.Date(2026, 6, 26, 12, 0, 0, 0, time.Local)
	writeSessionSnapshot(t, dataDir, "alpha", sessionFileData{
		Sessions: map[string]*sessionData{
			"s-keep":  {ID: "s-keep", Name: "keep", UpdatedAt: now, History: []core.HistoryEntry{{Role: "user", Content: "keep", Timestamp: now}}},
			"s-empty": {ID: "s-empty", Name: "empty", UpdatedAt: now.Add(-time.Minute)},
		},
		UserSessions: map[string][]string{
			"feishu:chat-a:user-1": {"s-keep"},
			"feishu:chat-a:user-2": {"s-empty"},
		},
	})
	writeSessionSnapshot(t, dataDir, "beta", sessionFileData{
		Sessions: map[string]*sessionData{
			"b-keep":  {ID: "b-keep", Name: "keep", UpdatedAt: now},
			"b-empty": {ID: "b-empty", Name: "empty", UpdatedAt: now.Add(-time.Minute)},
		},
		UserSessions: map[string][]string{
			"feishu:chat-b:user-1": {"b-keep"},
			"feishu:chat-b:user-2": {"b-empty"},
		},
	})

	stdout, stderr := captureP8CommandOutput(t, func() {
		runSessions([]string{"--data-dir", dataDir, "prune", "alpha", "--empty"})
	})
	if stderr != "" {
		t.Fatalf("prune stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, "Project alpha:") || !strings.Contains(stdout, "s-empty") || strings.Contains(stdout, "Project beta:") {
		t.Fatalf("prune output did not stay scoped to alpha:\n%s", stdout)
	}

	records, err := loadAllSessions(dataDir)
	if err != nil {
		t.Fatalf("load sessions after prune: %v", err)
	}
	ids := make(map[string]bool)
	for _, record := range records {
		ids[record.GlobalID] = true
	}
	if ids["alpha:s-empty"] {
		t.Fatalf("alpha empty duplicate was not pruned; records=%v", ids)
	}
	if !ids["alpha:s-keep"] || !ids["beta:b-keep"] || !ids["beta:b-empty"] {
		t.Fatalf("prune removed sessions outside expected scope; records=%v", ids)
	}
}

func writeSessionSnapshot(t *testing.T, dataDir, project string, data sessionFileData) {
	t.Helper()
	dir := filepath.Join(dataDir, "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions dir: %v", err)
	}
	if data.Sessions == nil {
		data.Sessions = map[string]*sessionData{}
	}
	if data.ActiveSession == nil {
		data.ActiveSession = map[string]string{}
	}
	if data.UserSessions == nil {
		data.UserSessions = map[string][]string{}
	}
	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		t.Fatalf("marshal session snapshot: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, project+".json"), raw, 0o644); err != nil {
		t.Fatalf("write session snapshot: %v", err)
	}
}

func captureP8CommandOutput(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()
	oldStdout := os.Stdout
	oldStderr := os.Stderr
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}
	os.Stdout = outW
	os.Stderr = errW
	defer func() {
		os.Stdout = oldStdout
		os.Stderr = oldStderr
	}()

	fn()

	if err := outW.Close(); err != nil {
		t.Fatalf("close stdout writer: %v", err)
	}
	if err := errW.Close(); err != nil {
		t.Fatalf("close stderr writer: %v", err)
	}
	var outBuf, errBuf bytes.Buffer
	if _, err := outBuf.ReadFrom(outR); err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	if _, err := errBuf.ReadFrom(errR); err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	if err := outR.Close(); err != nil {
		t.Fatalf("close stdout reader: %v", err)
	}
	if err := errR.Close(); err != nil {
		t.Fatalf("close stderr reader: %v", err)
	}
	return outBuf.String(), errBuf.String()
}
