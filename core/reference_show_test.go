package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildReferenceViewRequest_ModeSelection(t *testing.T) {
	ws := t.TempDir()
	file := filepath.Join(ws, "svc", "handler.go")
	dir := filepath.Join(ws, "docs", "spec.v1")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		raw  string
		mode referenceViewMode
	}{
		{name: "file", raw: file, mode: referenceViewFileHead},
		{name: "feishu", raw: file + ":12", mode: referenceViewContext},
		{name: "feishucol", raw: file + ":12:2", mode: referenceViewContext},
		{name: "range", raw: file + ":8-17", mode: referenceViewRange},
		{name: "hash", raw: file + "#L12", mode: referenceViewContext},
		{name: "markdown", raw: "[handler.go](" + file + "#L12)", mode: referenceViewContext},
		{name: "dir", raw: dir, mode: referenceViewDir},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := buildReferenceViewRequest(tc.raw, ws)
			if err != nil {
				t.Fatalf("buildReferenceViewRequest error: %v", err)
			}
			if req.Mode != tc.mode {
				t.Fatalf("mode = %q, want %q", req.Mode, tc.mode)
			}
		})
	}
}

func TestBuildReferenceViewRequest_DirectoryWithLocationFails(t *testing.T) {
	ws := t.TempDir()
	dir := filepath.Join(ws, "docs", "spec.v1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := buildReferenceViewRequest(dir+":12", ws); err == nil {
		t.Fatal("expected error for directory location reference")
	}
}

func TestRenderReferenceView_FileHeadAndContext(t *testing.T) {
	ws := t.TempDir()
	file := filepath.Join(ws, "svc", "handler.go")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	content := strings.Join([]string{
		"package svc",
		"",
		"func one() {}",
		"func two() {}",
		"func three() {}",
	}, "\n")
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	headReq, err := buildReferenceViewRequest(file, ws)
	if err != nil {
		t.Fatal(err)
	}
	head, err := renderReferenceView(headReq)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(head, "```go") {
		t.Fatalf("head output = %q, want go code fence", head)
	}
	if !strings.Contains(head, "package svc") {
		t.Fatalf("head output = %q, want file content", head)
	}

	ctxReq, err := buildReferenceViewRequest(file+":4", ws)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := renderReferenceView(ctxReq)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ctx, "func two() {}") {
		t.Fatalf("context output = %q, want nearby feishu", ctx)
	}
}

func TestRenderReferenceView_DirectoryList(t *testing.T) {
	ws := t.TempDir()
	dir := filepath.Join(ws, "docs", "spec.v1")
	if err := os.MkdirAll(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "alpha.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	req, err := buildReferenceViewRequest(dir, ws)
	if err != nil {
		t.Fatal(err)
	}
	out, err := renderReferenceView(req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "📁 docs/spec.v1/") {
		t.Fatalf("directory output = %q, want title", out)
	}
	if !strings.Contains(out, "- alpha.md") || !strings.Contains(out, "- subdir/") {
		t.Fatalf("directory output = %q, want entries", out)
	}
}

func TestRenderReferenceView_InvalidRequestsAndMissingPath(t *testing.T) {
	if _, err := renderReferenceView(nil); err == nil {
		t.Fatal("renderReferenceView(nil) returned nil error")
	}
	if _, err := renderReferenceView(&referenceViewRequest{}); err == nil {
		t.Fatal("renderReferenceView(empty request) returned nil error")
	}
	req := &referenceViewRequest{Ref: &localReference{pathOriginal: filepath.Join(t.TempDir(), "missing.go")}}
	if _, err := renderReferenceView(req); err == nil || !strings.Contains(err.Error(), "path does not exist") {
		t.Fatalf("renderReferenceView(missing) error = %v, want path does not exist", err)
	}
}

func TestRenderReferenceView_RangeAndTruncation(t *testing.T) {
	ws := t.TempDir()
	file := filepath.Join(ws, "notes.md")
	content := strings.Join([]string{"one", "two", "three", "four", "five"}, "\n")
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	req, err := buildReferenceViewRequest(file+":2-5", ws)
	if err != nil {
		t.Fatal(err)
	}
	req.MaxLines = 2
	out, err := renderReferenceView(req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Only showing the first 2 lines of the requested range.") {
		t.Fatalf("range output = %q, want truncation note", out)
	}
	if !strings.Contains(out, "```markdown\ntwo\nthree\n```") {
		t.Fatalf("range output = %q, want markdown fence with first range lines", out)
	}
}

func TestRenderReferenceDir_EmptyAndTruncated(t *testing.T) {
	ws := t.TempDir()
	empty := filepath.Join(ws, "empty")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	emptyReq, err := buildReferenceViewRequest(empty, ws)
	if err != nil {
		t.Fatal(err)
	}
	emptyOut, err := renderReferenceView(emptyReq)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(emptyOut, "(empty)") {
		t.Fatalf("empty dir output = %q", emptyOut)
	}

	dir := filepath.Join(ws, "many")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	truncReq, err := buildReferenceViewRequest(dir, ws)
	if err != nil {
		t.Fatal(err)
	}
	truncReq.MaxEntries = 2
	truncOut, err := renderReferenceView(truncReq)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(truncOut, "Only showing the first 2 entries.") {
		t.Fatalf("truncated dir output = %q", truncOut)
	}
}

func TestReferenceFileReaders_InvalidAndLimitedInputs(t *testing.T) {
	ws := t.TempDir()
	file := filepath.Join(ws, "script.sh")
	if err := os.WriteFile(file, []byte("l1\nl2\nl3\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, _, err := readFileRange(file, 0, 2, 10); err == nil {
		t.Fatal("readFileRange invalid start returned nil error")
	}
	if _, _, err := readFileRange(file, 2, 1, 10); err == nil {
		t.Fatal("readFileRange invalid end returned nil error")
	}

	head, truncated, err := readFileHead(file, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !truncated || strings.Join(head, ",") != "l1,l2" {
		t.Fatalf("readFileHead() = %v, %v, want first two truncated", head, truncated)
	}

	ctx, _, err := readFileContext(file, 1, 10, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(ctx, ",") != "l1,l2" {
		t.Fatalf("readFileContext() = %v, want first two lines", ctx)
	}
	if _, _, err := readFileContext(file, 0, 1, 1, 10); err == nil {
		t.Fatal("readFileContext invalid line returned nil error")
	}
}

func TestCodeFenceLanguageAndMinInt(t *testing.T) {
	cases := map[string]string{
		"main.go":       "go",
		"view.tsx":      "tsx",
		"script.py":     "python",
		"config.json":   "json",
		"workflow.yaml": "yaml",
		"run.sh":        "bash",
		"README.txt":    "",
	}
	for path, want := range cases {
		t.Run(path, func(t *testing.T) {
			if got := codeFenceLanguage(path); got != want {
				t.Fatalf("codeFenceLanguage(%q) = %q, want %q", path, got, want)
			}
		})
	}
	if got := minInt(7, 3); got != 3 {
		t.Fatalf("minInt(7, 3) = %d, want 3", got)
	}
	if got := minInt(2, 9); got != 2 {
		t.Fatalf("minInt(2, 9) = %d, want 2", got)
	}
}
