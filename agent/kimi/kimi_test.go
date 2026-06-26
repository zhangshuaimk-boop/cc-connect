package kimi

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chenhg5/cc-connect/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func skipUnlessKimiAvailable(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("kimi"); err != nil {
		t.Skipf("kimi CLI not in PATH, skipping: %v", err)
	}
}

func TestNormalizeMode(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"default", "default"},
		{"DEFAULT", "default"},
		{"yolo", "yolo"},
		{"YOLO", "yolo"},
		{"force", "yolo"},
		{"bypass", "yolo"},
		{"auto", "yolo"},
		{"plan", "plan"},
		{"quiet", "quiet"},
		{"", "default"},
		{"unknown", "default"},
	}

	for _, c := range cases {
		assert.Equal(t, c.expected, normalizeMode(c.input), "input: %s", c.input)
	}
}

func TestAgentNew(t *testing.T) {
	skipUnlessKimiAvailable(t)
	agentInf, err := New(map[string]any{
		"work_dir":     "/tmp",
		"model":        "kimi-k2",
		"mode":         "yolo",
		"timeout_mins": 15,
	})
	require.NoError(t, err)
	require.NotNil(t, agentInf)

	a := agentInf.(*Agent)
	assert.Equal(t, "kimi", a.Name())
	assert.Equal(t, "/tmp", a.GetWorkDir())
	assert.Equal(t, "yolo", a.GetMode())
	assert.Equal(t, "kimi-k2", a.GetModel())
}

// TestAgentFields verifies Name/WorkDir/Mode/Model without requiring
// the kimi CLI on PATH — constructs the struct directly.
func TestAgentFields(t *testing.T) {
	a := &Agent{
		workDir:   "/tmp",
		model:     "kimi-k2",
		mode:      "yolo",
		cmd:       "kimi",
		activeIdx: -1,
	}
	assert.Equal(t, "kimi", a.Name())
	assert.Equal(t, "Kimi", a.CLIDisplayName())
	assert.Equal(t, "kimi", a.CLIBinaryName())
	assert.Equal(t, "/tmp", a.GetWorkDir())
	assert.Equal(t, "yolo", a.GetMode())
	assert.Equal(t, "kimi-k2", a.GetModel())
}

func TestAgentNewMissingCLIAndDefaults(t *testing.T) {
	agentInf, err := New(map[string]any{
		"cmd":      "definitely-missing-kimi-cli",
		"work_dir": "",
	})
	if err == nil {
		t.Fatalf("New returned nil error and agent %#v, want missing CLI error", agentInf)
	}
	if !strings.Contains(err.Error(), "definitely-missing-kimi-cli") {
		t.Fatalf("missing CLI error = %v", err)
	}

	cliPath := filepath.Join(t.TempDir(), "kimi")
	if err := os.WriteFile(cliPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("WriteFile fake CLI: %v", err)
	}
	agentInf, err = New(map[string]any{
		"cmd":          cliPath + " --flag",
		"mode":         "quiet",
		"timeout_mins": float64(4),
	})
	if err != nil {
		t.Fatalf("New with fake CLI: %v", err)
	}
	a := agentInf.(*Agent)
	if a.workDir != "." || a.mode != "quiet" || a.cmd != cliPath {
		t.Fatalf("agent defaults = workDir %q mode %q cmd %q", a.workDir, a.mode, a.cmd)
	}
	if len(a.cliExtraArgs) != 1 || a.cliExtraArgs[0] != "--flag" {
		t.Fatalf("cliExtraArgs = %#v, want --flag", a.cliExtraArgs)
	}
	if a.timeout != 4*time.Minute {
		t.Fatalf("timeout = %v, want 4m", a.timeout)
	}
}

func TestAgentSetters(t *testing.T) {
	a := &Agent{workDir: "/tmp", mode: "default", activeIdx: -1}

	a.SetWorkDir("/new/path")
	assert.Equal(t, "/new/path", a.GetWorkDir())

	a.SetModel("kimi-k2-5")
	assert.Equal(t, "kimi-k2-5", a.GetModel())

	a.SetMode("plan")
	assert.Equal(t, "plan", a.GetMode())
}

func TestAgentPermissionModes(t *testing.T) {
	a := &Agent{}

	modes := a.PermissionModes()
	require.Len(t, modes, 4)
	assert.Equal(t, "default", modes[0].Key)
	assert.Equal(t, "yolo", modes[1].Key)
	assert.Equal(t, "plan", modes[2].Key)
	assert.Equal(t, "quiet", modes[3].Key)
}

func TestAgentProviderSwitcher(t *testing.T) {
	a := &Agent{workDir: "/tmp", activeIdx: -1}

	providers := []core.ProviderConfig{
		{Name: "moonshot", APIKey: "sk-123", Model: "kimi-provider", Env: map[string]string{"CUSTOM": "1"}},
		{Name: "custom", BaseURL: "https://api.example.com"},
	}
	a.SetProviders(providers)

	assert.Nil(t, a.GetActiveProvider())
	assert.False(t, a.SetActiveProvider("missing"))
	assert.True(t, a.SetActiveProvider("moonshot"))
	assert.Equal(t, "moonshot", a.GetActiveProvider().Name)
	assert.Equal(t, "kimi-provider", a.GetModel())
	env := envSliceToMap(a.providerEnvLocked())
	assert.Equal(t, "sk-123", env["KIMI_API_KEY"])
	assert.Equal(t, "1", env["CUSTOM"])

	list := a.ListProviders()
	require.Len(t, list, 2)
	assert.Equal(t, "moonshot", list[0].Name)

	assert.True(t, a.SetActiveProvider(""))
	assert.Nil(t, a.GetActiveProvider())
}

func TestAgentStartSession(t *testing.T) {
	skipUnlessKimiAvailable(t)
	agentInf, err := New(map[string]any{
		"work_dir":     "/tmp",
		"model":        "kimi-k2",
		"mode":         "default",
		"timeout_mins": 10,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	session, err := agentInf.StartSession(ctx, "test-session-id")
	require.NoError(t, err)
	require.NotNil(t, session)
	assert.True(t, session.Alive())
	assert.Equal(t, "test-session-id", session.CurrentSessionID())

	err = session.Close()
	assert.NoError(t, err)
	assert.False(t, session.Alive())
}

func TestAgentMemoryAndSkill(t *testing.T) {
	a := &Agent{workDir: "/tmp/my-project", activeIdx: -1}

	assert.Equal(t, "/tmp/my-project/AGENTS.md", a.ProjectMemoryFile())
	assert.NotEmpty(t, a.GlobalMemoryFile())

	skillDirs := a.SkillDirs()
	require.Len(t, skillDirs, 2)
	assert.Contains(t, skillDirs[0], ".kimi/skills")
	assert.Contains(t, skillDirs[1], ".kimi/skills")
}

func TestAgentAvailableModels(t *testing.T) {
	a := &Agent{workDir: "/tmp", activeIdx: -1}

	models := a.AvailableModels(context.Background())
	require.True(t, len(models) > 0)
}

func TestStartSessionFakeCLI_MergesEnvArgsAndPrompt(t *testing.T) {
	tmp := t.TempDir()
	capturePath := filepath.Join(tmp, "capture.txt")
	cliPath := writeKimiFakeCLI(t, capturePath)

	a := &Agent{
		workDir:      tmp,
		model:        "fallback-model",
		mode:         "plan",
		cmd:          cliPath,
		cliExtraArgs: []string{"--wrapped"},
		configEnv:    []string{"CONFIG_FLAG=from-config"},
		providers: []core.ProviderConfig{{
			Name:   "moonshot",
			APIKey: "provider-key",
			Model:  "provider-model",
			Env:    map[string]string{"PROVIDER_FLAG": "from-provider"},
		}},
		activeIdx: 0,
	}
	a.SetSessionEnv([]string{"SESSION_FLAG=from-session"})

	session, err := a.StartSession(context.Background(), "sid-1")
	require.NoError(t, err)
	defer session.Close()

	require.NoError(t, session.Send("hello", nil, nil))
	events := readKimiEventsUntilResult(t, session.Events(), capturePath)
	require.True(t, kimiHasEventType(events, core.EventText), "events=%#v", events)
	require.True(t, kimiHasEventType(events, core.EventResult), "events=%#v", events)

	_ = waitForKimiFileContents(t, capturePath,
		"args=--wrapped --print --output-format stream-json --plan --resume sid-1 --model provider-model --work-dir "+tmp+" --prompt hello",
		"KIMI_API_KEY=provider-key",
		"CONFIG_FLAG=from-config",
		"PROVIDER_FLAG=from-provider",
		"SESSION_FLAG=from-session",
	)
}

func TestKimiSessionListingAndDelete(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	base := filepath.Join(home, ".kimi", "sessions", "project-a")
	sessionDir := filepath.Join(base, "sid-1")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("MkdirAll sessionDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, "state.json"), []byte(`{"custom_title":"Custom title"}`), 0o644); err != nil {
		t.Fatalf("WriteFile state: %v", err)
	}
	contextData := strings.Join([]string{
		`{"role":"user","content":"first user line"}`,
		`{"role":"assistant","content":"reply"}`,
		`not-json`,
	}, "\n")
	if err := os.WriteFile(filepath.Join(sessionDir, "context.jsonl"), []byte(contextData), 0o644); err != nil {
		t.Fatalf("WriteFile context: %v", err)
	}
	archivedDir := filepath.Join(base, "archived")
	if err := os.MkdirAll(archivedDir, 0o755); err != nil {
		t.Fatalf("MkdirAll archived: %v", err)
	}
	if err := os.WriteFile(filepath.Join(archivedDir, "state.json"), []byte(`{"custom_title":"Archived","archived":true}`), 0o644); err != nil {
		t.Fatalf("WriteFile archived state: %v", err)
	}

	a := &Agent{workDir: t.TempDir()}
	sessions, err := a.ListSessions(context.Background())
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	assert.Equal(t, "sid-1", sessions[0].ID)
	assert.Equal(t, "first user line", sessions[0].Summary)
	assert.Equal(t, 2, sessions[0].MessageCount)

	assert.Equal(t, sessionDir, findKimiSessionDir("sid-1"))
	require.NoError(t, a.DeleteSession(context.Background(), "sid-1"))
	_, err = os.Stat(sessionDir)
	assert.True(t, os.IsNotExist(err), "session dir should be removed: %v", err)
	assert.Error(t, a.DeleteSession(context.Background(), "missing"))
}

func writeKimiFakeCLI(t *testing.T, capturePath string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kimi")
	script := fmt.Sprintf(`#!/bin/sh
tmp=%q.$$
trap 'rm -f "$tmp"' EXIT
{
  printf 'args=%%s\n' "$*"
  printf 'KIMI_API_KEY=%%s\n' "$KIMI_API_KEY"
  printf 'CONFIG_FLAG=%%s\n' "$CONFIG_FLAG"
  printf 'PROVIDER_FLAG=%%s\n' "$PROVIDER_FLAG"
  printf 'SESSION_FLAG=%%s\n' "$SESSION_FLAG"
} > "$tmp"
mv "$tmp" %q
printf '{"role":"assistant","content":[{"type":"text","text":"done"}]}\n'
printf 'To resume this session: kimi -r sid-2\n' >&2
`, capturePath, capturePath)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile fake CLI: %v", err)
	}
	return path
}

func readKimiEventsUntilResult(t *testing.T, ch <-chan core.Event, capturePath string) []core.Event {
	t.Helper()
	var events []core.Event
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	for {
		select {
		case evt, ok := <-ch:
			if !ok {
				t.Fatalf("events channel closed before result, events=%#v, capture=%s", events, readKimiFileForFailure(capturePath))
			}
			events = append(events, evt)
			if evt.Type == core.EventResult {
				return events
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for result, events=%#v, capture=%s", events, readKimiFileForFailure(capturePath))
		}
	}
}

func kimiHasEventType(events []core.Event, typ core.EventType) bool {
	for _, evt := range events {
		if evt.Type == typ {
			return true
		}
	}
	return false
}

func waitForKimiFileContents(t *testing.T, path string, wants ...string) string {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	var data []byte
	var err error
	for time.Now().Before(deadline) {
		data, err = os.ReadFile(path)
		if err == nil {
			text := string(data)
			missing := missingKimiSubstrings(text, wants)
			if len(missing) == 0 {
				return text
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	t.Fatalf("capture missing %v:\n%s", missingKimiSubstrings(string(data), wants), string(data))
	return ""
}

func missingKimiSubstrings(text string, wants []string) []string {
	var missing []string
	for _, want := range wants {
		if !strings.Contains(text, want) {
			missing = append(missing, want)
		}
	}
	return missing
}

func readKimiFileForFailure(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return err.Error()
	}
	return string(data)
}
