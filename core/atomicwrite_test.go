package core

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

func TestAtomicWriteFile_Basic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	data := []byte("hello world")

	if err := AtomicWriteFile(path, data, 0644); err != nil {
		t.Fatalf("AtomicWriteFile: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("got %q, want %q", got, data)
	}
}

func TestAtomicWriteFile_Overwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")

	if err := AtomicWriteFile(path, []byte("first"), 0644); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := AtomicWriteFile(path, []byte("second"), 0644); err != nil {
		t.Fatalf("second write: %v", err)
	}

	got, _ := os.ReadFile(path)
	if string(got) != "second" {
		t.Errorf("got %q, want %q", got, "second")
	}
}

func TestAtomicWriteFile_Permissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix file permissions not supported on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")

	if err := AtomicWriteFile(path, []byte("x"), 0600); err != nil {
		t.Fatalf("AtomicWriteFile: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("perm = %o, want 0600", perm)
	}
}

func TestAtomicWriteFile_NoTempLeftOnSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")

	_ = AtomicWriteFile(path, []byte("data"), 0644)

	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() != "test.txt" {
			t.Errorf("unexpected file left: %s", e.Name())
		}
	}
}

// TestAtomicWriteFile_NoTempLeftWhenRenameFails is a regression test for a
// `.tmp-*` leak in the rename-failure path. Before the fix, AtomicWriteFile
// returned the rename error directly without removing the tmp file it had
// just created — so repeated failures would litter the parent directory
// with stale `.tmp-*` files and confuse callers that scan that directory
// (cron store, session store, etc.).
//
// We force a rename failure by writing to a path that already exists as a
// directory; os.Rename refuses to replace a non-empty directory with a
// regular file on every supported platform.
func TestAtomicWriteFile_NoTempLeftWhenRenameFails(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "blocked")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("mkdir target dir: %v", err)
	}

	if err := AtomicWriteFile(target, []byte("payload"), 0o644); err == nil {
		t.Fatal("AtomicWriteFile should fail when target is an existing directory")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != "blocked" {
			t.Errorf("rename failure left orphan file %q in %s; cleanup is missing", e.Name(), dir)
		}
	}
}

func TestAtomicWriteFile_MissingParentDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "missing", "test.txt")

	if err := AtomicWriteFile(path, []byte("payload"), 0o644); err == nil {
		t.Fatal("AtomicWriteFile should fail when parent directory is missing")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("target should not be created, stat err = %v", err)
	}
}

func TestAtomicWriteFile_ReadOnlyDirFailsWithoutTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix directory permissions not supported on Windows")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("chmod read-only dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(dir, 0o755)
	})

	path := filepath.Join(dir, "test.txt")
	if err := AtomicWriteFile(path, []byte("payload"), 0o644); err == nil {
		t.Fatal("AtomicWriteFile should fail in a read-only directory")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("target should not be created, stat err = %v", err)
	}
}

func TestAtomicWriteFile_ConcurrentWritesLeaveCompletePayload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	payloads := [][]byte{
		[]byte("payload-00"),
		[]byte("payload-01"),
		[]byte("payload-02"),
		[]byte("payload-03"),
		[]byte("payload-04"),
		[]byte("payload-05"),
		[]byte("payload-06"),
		[]byte("payload-07"),
	}

	var wg sync.WaitGroup
	errCh := make(chan error, len(payloads))
	for _, payload := range payloads {
		payload := payload
		wg.Add(1)
		go func() {
			defer wg.Done()
			errCh <- AtomicWriteFile(path, payload, 0o644)
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("concurrent AtomicWriteFile returned error: %v", err)
		}
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	matched := false
	for _, payload := range payloads {
		if string(got) == string(payload) {
			matched = true
			break
		}
	}
	if !matched {
		t.Fatalf("final content = %q, want one complete payload", got)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != "test.txt" {
			t.Errorf("concurrent writes left unexpected file %q", e.Name())
		}
	}
}
