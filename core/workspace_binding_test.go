package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWorkspaceBindingManager_SaveLoad(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "bindings.json")
	channelKey := workspaceChannelKey("feishu", "C123")

	mgr := NewWorkspaceBindingManager(storePath)
	mgr.Bind("project:claude", channelKey, "my-channel", "/home/user/workspace/my-channel")

	b := mgr.Lookup("project:claude", channelKey)
	if b == nil {
		t.Fatal("expected binding, got nil")
	}
	if b.ChannelName != "my-channel" {
		t.Errorf("expected channel name 'my-channel', got %q", b.ChannelName)
	}
	if b.Workspace != "/home/user/workspace/my-channel" {
		t.Errorf("expected workspace path, got %q", b.Workspace)
	}

	// Reload from disk
	mgr2 := NewWorkspaceBindingManager(storePath)
	b2 := mgr2.Lookup("project:claude", channelKey)
	if b2 == nil {
		t.Fatal("expected binding after reload, got nil")
	}
	if b2.Workspace != "/home/user/workspace/my-channel" {
		t.Errorf("expected workspace path after reload, got %q", b2.Workspace)
	}
}

func TestWorkspaceBindingManager_Unbind(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "bindings.json")
	channelKey := workspaceChannelKey("feishu", "C123")

	mgr := NewWorkspaceBindingManager(storePath)
	mgr.Bind("project:claude", channelKey, "chan", "/path")
	mgr.Unbind("project:claude", channelKey)

	if b := mgr.Lookup("project:claude", channelKey); b != nil {
		t.Error("expected nil after unbind")
	}
}

func TestWorkspaceBindingManager_ListByProject(t *testing.T) {
	dir := t.TempDir()
	mgr := NewWorkspaceBindingManager(filepath.Join(dir, "bindings.json"))
	mgr.Bind("project:claude", workspaceChannelKey("feishu", "C1"), "chan1", "/path1")
	mgr.Bind("project:claude", workspaceChannelKey("feishu", "C2"), "chan2", "/path2")
	mgr.Bind("project:other", workspaceChannelKey("feishu", "C3"), "chan3", "/path3")

	list := mgr.ListByProject("project:claude")
	if len(list) != 2 {
		t.Errorf("expected 2 bindings, got %d", len(list))
	}
}

func TestWorkspaceBindingManager_LookupEffective(t *testing.T) {
	dir := t.TempDir()
	mgr := NewWorkspaceBindingManager(filepath.Join(dir, "bindings.json"))
	channelKey := workspaceChannelKey("feishu", "C1")

	mgr.Bind(sharedWorkspaceBindingsKey, channelKey, "shared-chan", "/shared")
	mgr.Bind("project:claude", channelKey, "local-chan", "/local")

	if b, key := mgr.LookupEffective("project:claude", channelKey); b == nil || key != "project:claude" || b.Workspace != "/local" {
		t.Fatalf("expected local override, got binding=%v key=%q", b, key)
	}

	if b, key := mgr.LookupEffective("project:other", channelKey); b == nil || key != sharedWorkspaceBindingsKey || b.Workspace != "/shared" {
		t.Fatalf("expected shared fallback, got binding=%v key=%q", b, key)
	}

	if b, key := mgr.LookupEffective("project:none", "missing"); b != nil || key != "" {
		t.Fatalf("expected nil binding, got binding=%v key=%q", b, key)
	}
}

func TestWorkspaceBindingManager_LoadSharedFromDisk(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "bindings.json")
	channelKey := workspaceChannelKey("feishu", "C1")

	mgr := NewWorkspaceBindingManager(storePath)
	mgr.Bind(sharedWorkspaceBindingsKey, channelKey, "shared-chan", "/shared")

	reloaded := NewWorkspaceBindingManager(storePath)
	if b, key := reloaded.LookupEffective("project:other", channelKey); b == nil || key != sharedWorkspaceBindingsKey || b.Workspace != "/shared" {
		t.Fatalf("expected shared binding after reload, got binding=%v key=%q", b, key)
	}
}

func TestWorkspaceBindingManager_RefreshesExternalChanges(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "bindings.json")
	channelKey := workspaceChannelKey("feishu", "C1")

	mgrA := NewWorkspaceBindingManager(storePath)
	mgrB := NewWorkspaceBindingManager(storePath)

	mgrA.Bind(sharedWorkspaceBindingsKey, channelKey, "shared-chan", "/shared")

	if b, key := mgrB.LookupEffective("project:other", channelKey); b == nil || key != sharedWorkspaceBindingsKey || b.Workspace != "/shared" {
		t.Fatalf("expected shared binding from external update, got binding=%v key=%q", b, key)
	}

	mgrB.Bind("project:other", channelKey, "local-chan", "/local")

	if b, key := mgrA.LookupEffective("project:other", channelKey); b == nil || key != "project:other" || b.Workspace != "/local" {
		t.Fatalf("expected local override from external update, got binding=%v key=%q", b, key)
	}

	mgrA.Unbind(sharedWorkspaceBindingsKey, channelKey)

	if b, key := mgrB.LookupEffective("project:none", channelKey); b != nil || key != "" {
		t.Fatalf("expected shared binding removal to propagate, got binding=%v key=%q", b, key)
	}
}

func TestWorkspaceBindingManager_LegacyFallbackForScopedLookup(t *testing.T) {
	dir := t.TempDir()
	mgr := NewWorkspaceBindingManager(filepath.Join(dir, "bindings.json"))

	mgr.Bind(sharedWorkspaceBindingsKey, "C1", "legacy-chan", "/shared")

	if b, key := mgr.LookupEffective("project:other", workspaceChannelKey("feishu", "C1")); b == nil || key != sharedWorkspaceBindingsKey || b.Workspace != "/shared" {
		t.Fatalf("expected legacy shared binding to be found by scoped lookup, got binding=%v key=%q", b, key)
	}
}

func TestWorkspaceBindingManager_UnbindScopedRemovesLegacyBinding(t *testing.T) {
	dir := t.TempDir()
	mgr := NewWorkspaceBindingManager(filepath.Join(dir, "bindings.json"))

	mgr.Bind("project:claude", "C1", "legacy-chan", "/shared")
	mgr.Unbind("project:claude", workspaceChannelKey("feishu", "C1"))

	if b := mgr.Lookup("project:claude", workspaceChannelKey("feishu", "C1")); b != nil {
		t.Fatalf("expected scoped unbind to remove legacy binding, got %+v", b)
	}
	if b := mgr.Lookup("project:claude", "C1"); b != nil {
		t.Fatalf("expected legacy binding to be removed, got %+v", b)
	}
}

func TestFlexTimeUnmarshalJSONVariants(t *testing.T) {
	var withZone FlexTime
	if err := json.Unmarshal([]byte(`"2026-06-26T10:11:12Z"`), &withZone); err != nil {
		t.Fatal(err)
	}
	if withZone.IsZero() {
		t.Fatal("expected RFC3339 timestamp to parse")
	}

	var local FlexTime
	if err := json.Unmarshal([]byte(`"2026-06-26 10:11:12"`), &local); err != nil {
		t.Fatal(err)
	}
	wantLocal := time.Date(2026, 6, 26, 10, 11, 12, 0, time.Local)
	if !local.Equal(wantLocal) {
		t.Fatalf("local timestamp = %v, want %v", local.Time, wantLocal)
	}

	for _, raw := range []string{`""`, `"not-a-time"`, `123`} {
		t.Run(raw, func(t *testing.T) {
			var ft FlexTime
			if err := json.Unmarshal([]byte(raw), &ft); err != nil {
				t.Fatal(err)
			}
			if !ft.IsZero() {
				t.Fatalf("expected %s to decode as zero time, got %v", raw, ft.Time)
			}
		})
	}
}

func TestWorkspaceBindingManager_CorruptFileCanBeRecoveredByNewBinding(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "bindings.json")
	if err := os.WriteFile(storePath, []byte("{not-json"), 0o644); err != nil {
		t.Fatal(err)
	}

	mgr := NewWorkspaceBindingManager(storePath)
	channelKey := workspaceChannelKey("feishu", "C1")
	if got := mgr.Lookup("project:claude", channelKey); got != nil {
		t.Fatalf("expected corrupt store to load no binding, got %+v", got)
	}

	mgr.Bind("project:claude", channelKey, "chan", "/workspace")

	reloaded := NewWorkspaceBindingManager(storePath)
	if got := reloaded.Lookup("project:claude", channelKey); got == nil || got.Workspace != "/workspace" || got.ChannelName != "chan" {
		t.Fatalf("expected recovered binding after save, got %+v", got)
	}
}

func TestWorkspaceBindingManager_RefreshFileDeletionResetsBindings(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "bindings.json")
	channelKey := workspaceChannelKey("feishu", "C1")

	mgr := NewWorkspaceBindingManager(storePath)
	mgr.Bind("project:claude", channelKey, "chan", "/workspace")
	if got := mgr.Lookup("project:claude", channelKey); got == nil {
		t.Fatal("expected binding before deleting store")
	}

	if err := os.Remove(storePath); err != nil {
		t.Fatal(err)
	}
	if got := mgr.Lookup("project:claude", channelKey); got != nil {
		t.Fatalf("expected bindings to reset after store deletion, got %+v", got)
	}
	if list := mgr.ListByProject("project:claude"); len(list) != 0 {
		t.Fatalf("expected empty project list after store deletion, got %+v", list)
	}
}

func TestWorkspaceBindingManager_EmptyStoreAndEmptyChannelKey(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "bindings.json")
	if err := os.WriteFile(storePath, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	mgr := NewWorkspaceBindingManager(storePath)
	if got := mgr.Lookup("project:claude", ""); got != nil {
		t.Fatalf("expected empty channel lookup to miss, got %+v", got)
	}
	if got := workspaceChannelKeyCandidates(""); got != nil {
		t.Fatalf("workspaceChannelKeyCandidates(\"\") = %#v, want nil", got)
	}
}
