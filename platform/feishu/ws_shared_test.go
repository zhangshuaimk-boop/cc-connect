package feishu

import (
	"sync"
	"testing"
)

func TestSharedWSGroup_RegisterAndAllPlatforms(t *testing.T) {
	// Clean up global state for test isolation.
	cleanup := func() {
		sharedWSMu.Lock()
		defer sharedWSMu.Unlock()
		for k := range sharedWSGroups {
			delete(sharedWSGroups, k)
		}
	}
	cleanup()
	defer cleanup()

	p1 := &Platform{appID: "cli_test", domain: "feishu.cn"}
	p2 := &Platform{appID: "cli_test", domain: "feishu.cn"}

	// Register first platform — should be primary.
	g1, isPrimary1 := registerSharedWS(p1)
	if !isPrimary1 {
		t.Fatal("first platform should be primary")
	}
	if len(g1.allPlatforms()) != 1 {
		t.Fatalf("expected 1 platform, got %d", len(g1.allPlatforms()))
	}

	// Register second platform — should be secondary, same group.
	g2, isPrimary2 := registerSharedWS(p2)
	if isPrimary2 {
		t.Fatal("second platform should not be primary")
	}
	if g1 != g2 {
		t.Fatal("both platforms should share the same group")
	}
	if len(g1.allPlatforms()) != 2 {
		t.Fatalf("expected 2 platforms, got %d", len(g1.allPlatforms()))
	}
}

func TestSharedWSGroup_Unregister(t *testing.T) {
	cleanup := func() {
		sharedWSMu.Lock()
		defer sharedWSMu.Unlock()
		for k := range sharedWSGroups {
			delete(sharedWSGroups, k)
		}
	}
	cleanup()
	defer cleanup()

	p1 := &Platform{appID: "cli_test", domain: "feishu.cn"}
	p2 := &Platform{appID: "cli_test", domain: "feishu.cn"}

	g, _ := registerSharedWS(p1)
	registerSharedWS(p2)

	// Unregister first — one remains.
	remaining := unregisterSharedWS(p1)
	if remaining != 1 {
		t.Fatalf("expected 1 remaining, got %d", remaining)
	}
	platforms := g.allPlatforms()
	if len(platforms) != 1 || platforms[0] != p2 {
		t.Fatal("expected only p2 to remain")
	}

	// Unregister last — group deleted.
	remaining = unregisterSharedWS(p2)
	if remaining != 0 {
		t.Fatalf("expected 0 remaining, got %d", remaining)
	}
	sharedWSMu.Lock()
	_, exists := sharedWSGroups[sharedWSKey("cli_test", "feishu.cn")]
	sharedWSMu.Unlock()
	if exists {
		t.Fatal("group should be deleted when empty")
	}
}

func TestSharedWSGroup_DifferentAppIDs(t *testing.T) {
	cleanup := func() {
		sharedWSMu.Lock()
		defer sharedWSMu.Unlock()
		for k := range sharedWSGroups {
			delete(sharedWSGroups, k)
		}
	}
	cleanup()
	defer cleanup()

	p1 := &Platform{appID: "cli_aaa", domain: "feishu.cn"}
	p2 := &Platform{appID: "cli_bbb", domain: "feishu.cn"}

	g1, isPrimary1 := registerSharedWS(p1)
	g2, isPrimary2 := registerSharedWS(p2)

	if !isPrimary1 || !isPrimary2 {
		t.Fatal("different app_ids should each be primary")
	}
	if g1 == g2 {
		t.Fatal("different app_ids should have separate groups")
	}

	unregisterSharedWS(p1)
	unregisterSharedWS(p2)
}

func TestSharedWSGroup_DifferentDomains(t *testing.T) {
	cleanup := func() {
		sharedWSMu.Lock()
		defer sharedWSMu.Unlock()
		for k := range sharedWSGroups {
			delete(sharedWSGroups, k)
		}
	}
	cleanup()
	defer cleanup()

	p1 := &Platform{appID: "cli_same", domain: "https://open.feishu.cn"}
	p2 := &Platform{appID: "cli_same", domain: "https://open.larksuite.com"}

	g1, isPrimary1 := registerSharedWS(p1)
	g2, isPrimary2 := registerSharedWS(p2)
	if !isPrimary1 || !isPrimary2 {
		t.Fatal("same app_id on different domains should each be primary")
	}
	if g1 == g2 {
		t.Fatal("different domains should have separate groups")
	}
}

func TestSharedWSGroup_UnregisterMissingAndDuplicateAreSafe(t *testing.T) {
	cleanup := func() {
		sharedWSMu.Lock()
		defer sharedWSMu.Unlock()
		for k := range sharedWSGroups {
			delete(sharedWSGroups, k)
		}
	}
	cleanup()
	defer cleanup()

	p1 := &Platform{appID: "cli_dup", domain: "feishu.cn"}
	p2 := &Platform{appID: "cli_dup", domain: "feishu.cn"}
	missing := &Platform{appID: "cli_missing", domain: "feishu.cn"}

	if remaining := unregisterSharedWS(missing); remaining != 0 {
		t.Fatalf("unregister missing group remaining = %d, want 0", remaining)
	}

	registerSharedWS(p1)
	registerSharedWS(p2)
	foreignSameKey := &Platform{appID: "cli_dup", domain: "feishu.cn"}
	if remaining := unregisterSharedWS(foreignSameKey); remaining != 2 {
		t.Fatalf("unregister unregistered platform on existing key remaining = %d, want 2", remaining)
	}

	if remaining := unregisterSharedWS(p1); remaining != 1 {
		t.Fatalf("first unregister remaining = %d, want 1", remaining)
	}
	if remaining := unregisterSharedWS(p1); remaining != 1 {
		t.Fatalf("duplicate unregister remaining = %d, want 1", remaining)
	}
	if remaining := unregisterSharedWS(p2); remaining != 0 {
		t.Fatalf("last unregister remaining = %d, want 0", remaining)
	}
}

func TestSharedWSGroup_AllPlatformsSnapshotIsIndependent(t *testing.T) {
	g := &sharedWSGroup{}
	p1 := &Platform{appID: "cli_snapshot", domain: "feishu.cn"}
	p2 := &Platform{appID: "cli_snapshot", domain: "feishu.cn"}
	g.platforms = []*Platform{p1, p2}

	snapshot := g.allPlatforms()
	if len(snapshot) != 2 {
		t.Fatalf("snapshot length = %d, want 2", len(snapshot))
	}
	snapshot[0] = nil

	next := g.allPlatforms()
	if next[0] != p1 || next[1] != p2 {
		t.Fatalf("mutating snapshot changed group contents: %#v", next)
	}
}

func TestSharedWSGroup_ConcurrentRegisterUnregister(t *testing.T) {
	cleanup := func() {
		sharedWSMu.Lock()
		defer sharedWSMu.Unlock()
		for k := range sharedWSGroups {
			delete(sharedWSGroups, k)
		}
	}
	cleanup()
	defer cleanup()

	const n = 24
	platforms := make([]*Platform, n)
	var wg sync.WaitGroup
	for i := range platforms {
		platforms[i] = &Platform{appID: "cli_concurrent", domain: "feishu.cn"}
		wg.Add(1)
		go func(p *Platform) {
			defer wg.Done()
			registerSharedWS(p)
		}(platforms[i])
	}
	wg.Wait()

	sharedWSMu.Lock()
	g := sharedWSGroups[sharedWSKey("cli_concurrent", "feishu.cn")]
	sharedWSMu.Unlock()
	if g == nil {
		t.Fatal("group was not created")
	}
	if got := len(g.allPlatforms()); got != n {
		t.Fatalf("registered platforms = %d, want %d", got, n)
	}

	for _, p := range platforms {
		wg.Add(1)
		go func(p *Platform) {
			defer wg.Done()
			unregisterSharedWS(p)
		}(p)
	}
	wg.Wait()

	sharedWSMu.Lock()
	_, exists := sharedWSGroups[sharedWSKey("cli_concurrent", "feishu.cn")]
	sharedWSMu.Unlock()
	if exists {
		t.Fatal("group should be deleted after concurrent unregister")
	}
}
