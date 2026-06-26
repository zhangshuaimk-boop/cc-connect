package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestProjectState_SaveLoadAndClear(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "projects", "demo.state.json")

	store := NewProjectStateStore(statePath)
	store.SetWorkDirOverride("/tmp/demo")
	store.Save()

	reloaded := NewProjectStateStore(statePath)
	if got := reloaded.WorkDirOverride(); got != "/tmp/demo" {
		t.Fatalf("WorkDirOverride() = %q, want %q", got, "/tmp/demo")
	}

	reloaded.ClearWorkDirOverride()
	reloaded.Save()

	cleared := NewProjectStateStore(statePath)
	if got := cleared.WorkDirOverride(); got != "" {
		t.Fatalf("WorkDirOverride() after clear = %q, want empty", got)
	}
}

func TestWorkspaceDirOverride(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "projects", "demo.state.json")
	workspaceA := "/tmp/workspace-a"
	workspaceB := "/tmp/workspace-b"

	store := NewProjectStateStore(statePath)
	store.SetWorkDirOverride("/tmp/global")
	store.SetWorkspaceDirOverride(workspaceA, "/tmp/workspace-a/override")
	store.SetWorkspaceDirOverride(workspaceB, "/tmp/workspace-b/override")
	store.Save()

	reloaded := NewProjectStateStore(statePath)
	if got := reloaded.WorkDirOverride(); got != "/tmp/global" {
		t.Fatalf("WorkDirOverride() = %q, want %q", got, "/tmp/global")
	}
	if got := reloaded.WorkspaceDirOverride(workspaceA); got != "/tmp/workspace-a/override" {
		t.Fatalf("WorkspaceDirOverride(%q) = %q, want %q", workspaceA, got, "/tmp/workspace-a/override")
	}
	if got := reloaded.WorkspaceDirOverride(workspaceB); got != "/tmp/workspace-b/override" {
		t.Fatalf("WorkspaceDirOverride(%q) = %q, want %q", workspaceB, got, "/tmp/workspace-b/override")
	}
	if got := reloaded.WorkspaceDirOverride("/tmp/missing"); got != "" {
		t.Fatalf("WorkspaceDirOverride(missing) = %q, want empty", got)
	}

	reloaded.ClearWorkspaceDirOverride(workspaceA)
	reloaded.Save()

	cleared := NewProjectStateStore(statePath)
	if got := cleared.WorkDirOverride(); got != "/tmp/global" {
		t.Fatalf("WorkDirOverride() after workspace clear = %q, want %q", got, "/tmp/global")
	}
	if got := cleared.WorkspaceDirOverride(workspaceA); got != "" {
		t.Fatalf("WorkspaceDirOverride(%q) after clear = %q, want empty", workspaceA, got)
	}
	if got := cleared.WorkspaceDirOverride(workspaceB); got != "/tmp/workspace-b/override" {
		t.Fatalf("WorkspaceDirOverride(%q) after clearing other workspace = %q, want %q", workspaceB, got, "/tmp/workspace-b/override")
	}
}

func TestWorkspaceModelOverride(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "projects", "demo.state.json")
	workspaceA := "/tmp/workspace-a"
	workspaceB := "/tmp/workspace-b"

	store := NewProjectStateStore(statePath)
	store.SetWorkspaceModelOverride(workspaceA, "opus")
	store.SetWorkspaceModelOverride(workspaceB, "sonnet")
	store.Save()

	reloaded := NewProjectStateStore(statePath)
	if got := reloaded.WorkspaceModelOverride(workspaceA); got != "opus" {
		t.Fatalf("WorkspaceModelOverride(%q) = %q, want %q", workspaceA, got, "opus")
	}
	if got := reloaded.WorkspaceModelOverride(workspaceB); got != "sonnet" {
		t.Fatalf("WorkspaceModelOverride(%q) = %q, want %q", workspaceB, got, "sonnet")
	}

	reloaded.ClearWorkspaceModelOverride(workspaceA)
	reloaded.Save()

	cleared := NewProjectStateStore(statePath)
	if got := cleared.WorkspaceModelOverride(workspaceA); got != "" {
		t.Fatalf("WorkspaceModelOverride(%q) after clear = %q, want empty", workspaceA, got)
	}
	if got := cleared.WorkspaceModelOverride(workspaceB); got != "sonnet" {
		t.Fatalf("WorkspaceModelOverride(%q) after clearing other workspace = %q, want %q", workspaceB, got, "sonnet")
	}
}

func TestProjectState_ClearMissingOverridesNoops(t *testing.T) {
	store := NewProjectStateStore("")

	store.ClearWorkspaceDirOverride("/tmp/missing")
	store.ClearWorkspaceModelOverride("/tmp/missing")
	store.SetWorkspaceModelOverride("/tmp/missing", "")

	if got := store.WorkspaceDirOverride("/tmp/missing"); got != "" {
		t.Fatalf("WorkspaceDirOverride() = %q, want empty", got)
	}
	if got := store.WorkspaceModelOverride("/tmp/missing"); got != "" {
		t.Fatalf("WorkspaceModelOverride() = %q, want empty", got)
	}
}

func TestProjectState_SetEmptyModelClearsAndPersistsNilMap(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "projects", "demo.state.json")
	workspace := "/tmp/workspace-a"

	store := NewProjectStateStore(statePath)
	store.SetWorkspaceModelOverride(workspace, "opus")
	store.Save()

	store.SetWorkspaceModelOverride(workspace, "")
	store.Save()

	reloaded := NewProjectStateStore(statePath)
	if got := reloaded.WorkspaceModelOverride(workspace); got != "" {
		t.Fatalf("WorkspaceModelOverride() after empty set = %q, want empty", got)
	}

	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["workspace_model_overrides"]; ok {
		t.Fatalf("workspace_model_overrides should be omitted after final clear, json=%s", data)
	}
}

func TestProjectState_CorruptFileStartsEmptyAndCanRecover(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "projects", "demo.state.json")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, []byte("{not-json"), 0o644); err != nil {
		t.Fatal(err)
	}

	store := NewProjectStateStore(statePath)
	if got := store.WorkDirOverride(); got != "" {
		t.Fatalf("WorkDirOverride() from corrupt state = %q, want empty", got)
	}
	if got := store.WorkspaceDirOverride("/tmp/workspace-a"); got != "" {
		t.Fatalf("WorkspaceDirOverride() from corrupt state = %q, want empty", got)
	}

	store.SetWorkDirOverride("/tmp/recovered")
	store.SetWorkspaceDirOverride("/tmp/workspace-a", "/tmp/recovered/a")
	store.Save()

	reloaded := NewProjectStateStore(statePath)
	if got := reloaded.WorkDirOverride(); got != "/tmp/recovered" {
		t.Fatalf("WorkDirOverride() after recovery = %q, want %q", got, "/tmp/recovered")
	}
	if got := reloaded.WorkspaceDirOverride("/tmp/workspace-a"); got != "/tmp/recovered/a" {
		t.Fatalf("WorkspaceDirOverride() after recovery = %q, want %q", got, "/tmp/recovered/a")
	}
}

func TestProjectState_SaveWithoutPathIsInMemoryOnly(t *testing.T) {
	store := NewProjectStateStore("")
	store.SetWorkDirOverride("/tmp/in-memory")
	store.SetWorkspaceDirOverride("/tmp/workspace-a", "/tmp/override-a")
	store.SetWorkspaceModelOverride("/tmp/workspace-a", "sonnet")
	store.Save()

	if got := store.WorkDirOverride(); got != "/tmp/in-memory" {
		t.Fatalf("WorkDirOverride() = %q, want in-memory value", got)
	}
	if got := store.WorkspaceDirOverride("/tmp/workspace-a"); got != "/tmp/override-a" {
		t.Fatalf("WorkspaceDirOverride() = %q, want in-memory value", got)
	}
	if got := store.WorkspaceModelOverride("/tmp/workspace-a"); got != "sonnet" {
		t.Fatalf("WorkspaceModelOverride() = %q, want in-memory value", got)
	}
}
