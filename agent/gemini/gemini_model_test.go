package gemini

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chenhg5/cc-connect/core"
)

func TestAgentAccessorsProvidersAndDirs(t *testing.T) {
	workDir := filepath.Join(t.TempDir(), "project")
	a := &Agent{
		workDir:   workDir,
		model:     "gemini-default",
		mode:      "default",
		cmd:       "gemini",
		activeIdx: -1,
	}

	if got := a.Name(); got != "gemini" {
		t.Fatalf("Name() = %q, want gemini", got)
	}
	a.SetWorkDir(workDir + "-next")
	if got := a.GetWorkDir(); got != workDir+"-next" {
		t.Fatalf("GetWorkDir() after SetWorkDir = %q", got)
	}
	a.SetModel("gemini-next")
	if got := a.GetModel(); got != "gemini-next" {
		t.Fatalf("GetModel() = %q, want gemini-next", got)
	}
	a.SetMode("autoedit")
	if got := a.GetMode(); got != "auto_edit" {
		t.Fatalf("GetMode() = %q, want auto_edit", got)
	}
	if err := a.Stop(); err != nil {
		t.Fatalf("Stop() = %v", err)
	}
	if got := a.CompressCommand(); got != "" {
		t.Fatalf("CompressCommand() = %q, want empty", got)
	}

	modes := a.PermissionModes()
	if len(modes) != 4 || modes[0].Key != "default" || modes[1].Key != "auto_edit" || modes[2].Key != "yolo" || modes[3].Key != "plan" {
		t.Fatalf("PermissionModes() = %#v", modes)
	}

	providers := []core.ProviderConfig{
		{Name: "p1", APIKey: "key-1", Model: "model-1", Env: map[string]string{"CUSTOM": "1"}},
		{Name: "p2", Model: "model-2"},
	}
	a.SetProviders(providers)
	if got := a.ListProviders(); len(got) != 2 || got[0].Name != "p1" {
		t.Fatalf("ListProviders() = %#v", got)
	}
	if a.GetActiveProvider() != nil {
		t.Fatal("GetActiveProvider() before switch should be nil")
	}
	if a.SetActiveProvider("missing") {
		t.Fatal("SetActiveProvider(missing) returned true")
	}
	if !a.SetActiveProvider("p1") {
		t.Fatal("SetActiveProvider(p1) returned false")
	}
	if got := a.GetActiveProvider(); got == nil || got.Name != "p1" {
		t.Fatalf("GetActiveProvider() = %#v, want p1", got)
	}
	a.mu.Lock()
	env := envEntriesToMap(a.providerEnvLocked())
	a.mu.Unlock()
	if env["GEMINI_API_KEY"] != "key-1" || env["CUSTOM"] != "1" {
		t.Fatalf("provider env = %#v", env)
	}
	if got := a.GetModel(); got != "model-1" {
		t.Fatalf("GetModel() with active provider = %q, want model-1", got)
	}
	if !a.SetActiveProvider("") || a.GetActiveProvider() != nil {
		t.Fatal("clearing active provider failed")
	}

	commandDirs := a.CommandDirs()
	if len(commandDirs) != 2 || !strings.HasSuffix(commandDirs[0], ".gemini/commands") {
		t.Fatalf("CommandDirs() = %#v", commandDirs)
	}
	skillDirs := a.SkillDirs()
	if len(skillDirs) != 2 || !strings.HasSuffix(skillDirs[0], ".gemini/skills") {
		t.Fatalf("SkillDirs() = %#v", skillDirs)
	}
	if got := a.ProjectMemoryFile(); !strings.HasSuffix(got, "GEMINI.md") {
		t.Fatalf("ProjectMemoryFile() = %q", got)
	}
	if got := a.GlobalMemoryFile(); !strings.HasSuffix(got, filepath.Join(".gemini", "GEMINI.md")) {
		t.Fatalf("GlobalMemoryFile() = %q", got)
	}
}

func TestNewMissingCLIAndDefaults(t *testing.T) {
	agentInf, err := New(map[string]any{
		"cmd":      "definitely-missing-gemini-cli",
		"work_dir": "",
	})
	if err == nil {
		t.Fatalf("New returned nil error and agent %#v, want missing CLI error", agentInf)
	}
	if !strings.Contains(err.Error(), "definitely-missing-gemini-cli") {
		t.Fatalf("missing CLI error = %v", err)
	}

	cliPath := filepath.Join(t.TempDir(), "gemini")
	if err := os.WriteFile(cliPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("WriteFile fake CLI: %v", err)
	}
	agentInf, err = New(map[string]any{
		"cmd":          cliPath + " --flag",
		"mode":         "acceptedits",
		"timeout_mins": 3,
	})
	if err != nil {
		t.Fatalf("New with fake CLI: %v", err)
	}
	a := agentInf.(*Agent)
	if a.workDir != "." || a.mode != "auto_edit" || a.cmd != cliPath {
		t.Fatalf("agent defaults = workDir %q mode %q cmd %q", a.workDir, a.mode, a.cmd)
	}
	if len(a.cliExtraArgs) != 1 || a.cliExtraArgs[0] != "--flag" {
		t.Fatalf("cliExtraArgs = %#v, want --flag", a.cliExtraArgs)
	}
	if a.timeout != 3*time.Minute {
		t.Fatalf("timeout = %v, want 3m", a.timeout)
	}
}

func TestNormalizeMode(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"default", "default"},
		{"YOLO", "yolo"},
		{"auto", "yolo"},
		{"acceptedits", "auto_edit"},
		{"edit", "auto_edit"},
		{"plan", "plan"},
		{"unknown", "default"},
	}
	for _, tt := range tests {
		if got := normalizeMode(tt.input); got != tt.want {
			t.Fatalf("normalizeMode(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestAvailableModelsUsesConfiguredAndFallback(t *testing.T) {
	a := &Agent{
		providers: []core.ProviderConfig{{
			Name:   "configured",
			Models: []core.ModelOption{{Name: "configured-model"}},
		}},
		activeIdx: 0,
	}
	if got := a.AvailableModels(context.Background()); len(got) != 1 || got[0].Name != "configured-model" {
		t.Fatalf("AvailableModels configured = %#v", got)
	}

	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")
	a = &Agent{activeIdx: -1}
	got := a.AvailableModels(context.Background())
	if len(got) == 0 || got[0].Name != "gemini-3.1-pro-preview" {
		t.Fatalf("AvailableModels fallback = %#v", got)
	}
}

func TestConfiguredModels_BoundaryConditions(t *testing.T) {
	a := &Agent{
		providers: []core.ProviderConfig{
			{Models: []core.ModelOption{{Name: "first"}}},
			{Models: []core.ModelOption{{Name: "second"}}},
		},
	}

	tests := []struct {
		name      string
		activeIdx int
		wantNil   bool
		wantName  string
	}{
		{name: "negative index", activeIdx: -1, wantNil: true},
		{name: "out of range", activeIdx: 2, wantNil: true},
		{name: "valid index", activeIdx: 1, wantName: "second"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a.activeIdx = tt.activeIdx
			got := a.configuredModels()
			if tt.wantNil {
				if got != nil {
					t.Fatalf("configuredModels() = %v, want nil", got)
				}
				return
			}
			if len(got) != 1 || got[0].Name != tt.wantName {
				t.Fatalf("configuredModels() = %v, want %q", got, tt.wantName)
			}
		})
	}
}

func TestGetModel_PrefersActiveProviderModel(t *testing.T) {
	a := &Agent{
		model: "gemini-2.5-flash",
		providers: []core.ProviderConfig{
			{Name: "google", Model: "gemini-2.5-pro"},
		},
		activeIdx: 0,
	}

	if got := a.GetModel(); got != "gemini-2.5-pro" {
		t.Fatalf("GetModel() = %q, want gemini-2.5-pro", got)
	}
}

func envEntriesToMap(env []string) map[string]string {
	out := make(map[string]string, len(env))
	for _, entry := range env {
		k, v, ok := strings.Cut(entry, "=")
		if ok {
			out[k] = v
		}
	}
	return out
}
