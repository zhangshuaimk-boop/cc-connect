package core

import (
	"fmt"
	"sync"
	"testing"
)

func TestPeerRegistryResolvePriority(t *testing.T) {
	r := NewPeerRegistry(map[string]string{"cli_a": "project-a"})

	if got, ok := r.Resolve("cli_a"); !ok || got != "project-a" {
		t.Fatalf("pending Resolve = (%q, %v), want (project-a, true)", got, ok)
	}
	if _, ok := r.Resolve("cli_unknown"); ok {
		t.Fatal("unknown Resolve ok = true, want false")
	}

	r.UpdateAPIName("cli_a", "API A")
	if got, ok := r.Resolve("cli_a"); !ok || got != "API A" {
		t.Fatalf("api Resolve = (%q, %v), want (API A, true)", got, ok)
	}
}

func TestPeerRegistryResetFromConfigDropsStaleAndUpdatesFallback(t *testing.T) {
	r := NewPeerRegistry(map[string]string{"cli_a": "old", "cli_stale": "stale"})
	r.UpdateAPIName("cli_a", "Old API")

	r.ResetFromConfig(map[string]string{"cli_a": "new", "cli_b": "project-b"})

	if got, ok := r.Resolve("cli_a"); !ok || got != "Old API" {
		t.Fatalf("reset cli_a = (%q, %v), want (Old API, true)", got, ok)
	}
	snapshot := r.snapshot.Load()
	if snapshot == nil {
		t.Fatal("snapshot is nil")
	}
	if got := snapshot.ByAppID["cli_a"].Fallback; got != "new" {
		t.Fatalf("fallback after reset = %q, want new", got)
	}
	if _, ok := r.Resolve("cli_stale"); ok {
		t.Fatal("stale app remained after reset")
	}
	if got, ok := r.Resolve("cli_b"); !ok || got != "project-b" {
		t.Fatalf("new app = (%q, %v), want (project-b, true)", got, ok)
	}
}

func TestPeerRegistryConcurrentReadWrite(t *testing.T) {
	r := NewPeerRegistry(map[string]string{"cli_a": "project-a"})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				r.Resolve("cli_a")
				r.Resolve(fmt.Sprintf("cli_%d", id))
			}
		}(i)
	}
	for i := 0; i < 100; i++ {
		r.UpdateAPIName("cli_a", fmt.Sprintf("API %d", i))
		r.ResetFromConfig(map[string]string{"cli_a": "project-a"})
	}
	wg.Wait()
}

func BenchmarkPeerRegistryResolve(b *testing.B) {
	r := NewPeerRegistry(map[string]string{"cli_a": "project-a"})
	r.UpdateAPIName("cli_a", "API A")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		r.Resolve("cli_a")
	}
}
