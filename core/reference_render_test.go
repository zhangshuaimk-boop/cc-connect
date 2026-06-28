package core

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTransformLocalReferences_DisabledWithoutNormalizeAgents(t *testing.T) {
	cfg := ReferenceRenderCfg{
		RenderPlatforms: []string{"feishu"},
		DisplayPath:     "basename",
		MarkerStyle:     "none",
		EnclosureStyle:  "none",
	}
	input := "See /root/code/demo/src/app.ts:42"
	got := TransformLocalReferences(input, cfg, "codex", "feishu", "/root/code/demo")
	if got != input {
		t.Fatalf("TransformLocalReferences() = %q, want unchanged %q", got, input)
	}
}

func TestTransformLocalReferences_UsesAllScopes(t *testing.T) {
	cfg := ReferenceRenderCfg{
		NormalizeAgents: []string{"all"},
		RenderPlatforms: []string{"all"},
		DisplayPath:     "basename",
		MarkerStyle:     "emoji",
		EnclosureStyle:  "code",
	}
	got := TransformLocalReferences("See /root/code/demo/src/app.ts:42", cfg, "codex", "feishu", "/root/code/demo")
	if !strings.Contains(got, "📄 `app.ts:42`") {
		t.Fatalf("TransformLocalReferences() = %q, want rendered basename reference", got)
	}
}

func TestTransformLocalReferences_PreservesWebMarkdownLinks(t *testing.T) {
	cfg := ReferenceRenderCfg{
		NormalizeAgents: []string{"codex"},
		RenderPlatforms: []string{"feishu"},
		DisplayPath:     "basename",
		MarkerStyle:     "none",
		EnclosureStyle:  "none",
	}
	input := "Docs: [OpenAI](https://openai.com/) and [app.ts](/root/code/demo/src/app.ts#L42)"
	got := TransformLocalReferences(input, cfg, "codex", "feishu", "/root/code/demo")
	if !strings.Contains(got, "[OpenAI](https://openai.com/)") {
		t.Fatalf("TransformLocalReferences() = %q, want web link preserved", got)
	}
	if !strings.Contains(got, "app.ts#L42") {
		t.Fatalf("TransformLocalReferences() = %q, want local hash-feishu reference rendered", got)
	}
}

func TestTransformLocalReferences_PreservesInlineCodePathRange(t *testing.T) {
	cfg := ReferenceRenderCfg{
		NormalizeAgents: []string{"claudecode"},
		RenderPlatforms: []string{"feishu"},
		DisplayPath:     "dirname_basename",
		MarkerStyle:     "ascii",
		EnclosureStyle:  "code",
	}
	got := TransformLocalReferences("Inspect `/root/.claude/settings.json:5-10` next.", cfg, "claudecode", "feishu", "/root")
	want := "[FILE] `.claude/settings.json:5-10`"
	if !strings.Contains(got, want) {
		t.Fatalf("TransformLocalReferences() = %q, want substring %q", got, want)
	}
}

func TestTransformLocalReferences_PreservesWebMarkdownLinksAfterInlineCodeReference(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("TransformLocalReferences path handling assumes Unix separators")
	}
	cfg := ReferenceRenderCfg{
		NormalizeAgents: []string{"claudecode"},
		RenderPlatforms: []string{"feishu"},
		DisplayPath:     "relative",
		MarkerStyle:     "emoji",
		EnclosureStyle:  "code",
	}
	input := "`/root/code/.claude/settings.json:5-10`\n[OpenAI](https://openai.com/)"
	got := TransformLocalReferences(input, cfg, "claudecode", "feishu", "/root/code")
	want := "📄 `.claude/settings.json:5-10`\n[OpenAI](https://openai.com/)"
	if got != want {
		t.Fatalf("TransformLocalReferences() = %q, want %q", got, want)
	}
}

func TestTransformLocalReferences_SmartDisplayFallsBackOnBasenameCollision(t *testing.T) {
	cfg := ReferenceRenderCfg{
		NormalizeAgents: []string{"codex"},
		RenderPlatforms: []string{"feishu"},
		DisplayPath:     "smart",
		MarkerStyle:     "none",
		EnclosureStyle:  "none",
	}
	input := "Compare /root/code/demo/src/app.ts and /root/code/demo/tests/app.ts"
	got := TransformLocalReferences(input, cfg, "codex", "feishu", "/root/code/demo")
	if !strings.Contains(got, "src/app.ts") || !strings.Contains(got, "tests/app.ts") {
		t.Fatalf("TransformLocalReferences() = %q, want dirname+basename for both colliding refs", got)
	}
}

func TestTransformLocalReferences_RelativeDisplayUsesWorkspace(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("TransformLocalReferences path handling assumes Unix separators")
	}
	cfg := ReferenceRenderCfg{
		NormalizeAgents: []string{"codex"},
		RenderPlatforms: []string{"feishu"},
		DisplayPath:     "relative",
		MarkerStyle:     "emoji",
		EnclosureStyle:  "code",
	}
	got := TransformLocalReferences("Look at /root/code/demo/src/app.ts:42:7", cfg, "codex", "feishu", "/root/code/demo")
	want := "📄 `src/app.ts:42:7`"
	if !strings.Contains(got, want) {
		t.Fatalf("TransformLocalReferences() = %q, want substring %q", got, want)
	}
}

func TestTransformLocalReferences_RelativeInputIsNotSplitByAbsoluteMatcher(t *testing.T) {
	cfg := ReferenceRenderCfg{
		NormalizeAgents: []string{"codex"},
		RenderPlatforms: []string{"feishu"},
		DisplayPath:     "relative",
		MarkerStyle:     "emoji",
		EnclosureStyle:  "code",
	}
	input := "See lean-steward/src/lean_topo_steward/prompting/instructions/global_instructions.py:42"
	got := TransformLocalReferences(input, cfg, "codex", "feishu", "/root/code")
	want := "See 📄 `lean-steward/src/lean_topo_steward/prompting/instructions/global_instructions.py:42`"
	if got != want {
		t.Fatalf("TransformLocalReferences() = %q, want %q", got, want)
	}
}

func TestTransformLocalReferences_ChineseListSeparatorsDoNotMergeCandidates(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("TransformLocalReferences path handling assumes Unix separators")
	}
	workspace := t.TempDir()
	filePath := filepath.Join(workspace, "demo-repo", "README")
	profileDir := filepath.Join(workspace, "demo-repo", "src", "components", "profile")
	profileExtDir := filepath.Join(workspace, "demo-repo", "src", "components", "profile.ts")
	specDir := filepath.Join(workspace, "demo-repo", "docs", "spec.v1")
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("MkdirAll(file dir) error: %v", err)
	}
	if err := os.WriteFile(filePath, []byte("readme"), 0o644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}
	for _, dir := range []string{profileDir, profileExtDir, specDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error: %v", dir, err)
		}
	}

	cfg := ReferenceRenderCfg{
		NormalizeAgents: []string{"claudecode"},
		RenderPlatforms: []string{"feishu"},
		DisplayPath:     "relative",
		MarkerStyle:     "emoji",
		EnclosureStyle:  "code",
	}
	input := "第 1 步：正在处理路径 demo-repo/README、" + profileDir + "、" + profileExtDir + "、" + specDir + "。"
	got := TransformLocalReferences(input, cfg, "claudecode", "feishu", workspace)
	want := "第 1 步：正在处理路径 📄 `demo-repo/README`、📁 `demo-repo/src/components/profile/`、📁 `demo-repo/src/components/profile.ts/`、📁 `demo-repo/docs/spec.v1/`。"
	if got != want {
		t.Fatalf("TransformLocalReferences() = %q, want %q", got, want)
	}
}

func TestTransformLocalReferences_ExistingDirectoryWithoutTrailingSlashIsDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("TransformLocalReferences path handling assumes Unix separators")
	}
	workspace := t.TempDir()
	dirPath := filepath.Join(workspace, "demo-repo", "src", "components")
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		t.Fatalf("MkdirAll() error: %v", err)
	}

	cfg := ReferenceRenderCfg{
		NormalizeAgents: []string{"codex"},
		RenderPlatforms: []string{"feishu"},
		DisplayPath:     "relative",
		MarkerStyle:     "emoji",
		EnclosureStyle:  "code",
	}
	got := TransformLocalReferences("Dir "+dirPath, cfg, "codex", "feishu", workspace)
	want := "Dir 📁 `demo-repo/src/components/`"
	if got != want {
		t.Fatalf("TransformLocalReferences() = %q, want %q", got, want)
	}
}

func TestTransformLocalReferences_WorkspaceRootDisplaysAsRelativeRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("TransformLocalReferences path handling assumes Unix separators")
	}
	workspace := t.TempDir()
	cfg := ReferenceRenderCfg{
		NormalizeAgents: []string{"codex"},
		RenderPlatforms: []string{"feishu"},
		DisplayPath:     "relative",
		MarkerStyle:     "emoji",
		EnclosureStyle:  "code",
	}
	got := TransformLocalReferences("Root "+workspace, cfg, "codex", "feishu", workspace)
	want := "Root 📁 `./`"
	if got != want {
		t.Fatalf("TransformLocalReferences() = %q, want %q", got, want)
	}
}

func TestTransformLocalReferences_UnknownNoExtPathKeepsNoMarker(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("TransformLocalReferences path handling assumes Unix separators")
	}
	workspace := t.TempDir()
	unknown := filepath.Join(workspace, "mysterypath")
	cfg := ReferenceRenderCfg{
		NormalizeAgents: []string{"codex"},
		RenderPlatforms: []string{"feishu"},
		DisplayPath:     "relative",
		MarkerStyle:     "emoji",
		EnclosureStyle:  "code",
	}
	got := TransformLocalReferences("Unknown "+unknown, cfg, "codex", "feishu", workspace)
	want := "Unknown `mysterypath`"
	if got != want {
		t.Fatalf("TransformLocalReferences() = %q, want %q", got, want)
	}
}

func TestTransformLocalReferences_ProtectsBareURLsAndCodeFences(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("TransformLocalReferences path handling assumes Unix separators")
	}
	workspace := t.TempDir()
	filePath := filepath.Join(workspace, "src", "app.go")
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filePath, []byte("package app\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := ReferenceRenderCfg{
		NormalizeAgents: []string{"codex"},
		RenderPlatforms: []string{"feishu"},
		DisplayPath:     "relative",
		MarkerStyle:     "ascii",
		EnclosureStyle:  "bracket",
	}
	input := strings.Join([]string{
		"Link https://example.com/src/app.go and " + filePath + ":1",
		"```",
		filePath + ":1",
		"```",
	}, "\n")
	got := TransformLocalReferences(input, cfg, "codex", "feishu", workspace)

	if !strings.Contains(got, "https://example.com/src/app.go") {
		t.Fatalf("TransformLocalReferences() = %q, want bare URL preserved", got)
	}
	if !strings.Contains(got, "[FILE] [src/app.go:1]") {
		t.Fatalf("TransformLocalReferences() = %q, want local file rendered", got)
	}
	if !strings.Contains(got, "```\n"+filePath+":1\n```") {
		t.Fatalf("TransformLocalReferences() = %q, want fenced reference unchanged", got)
	}
}

func TestTransformLocalReferences_UnsupportedScopesAndBoundaries(t *testing.T) {
	cfg := ReferenceRenderCfg{
		NormalizeAgents: []string{"unknown", "codex", "codex"},
		RenderPlatforms: []string{"slack", "feishu"},
		DisplayPath:     "basename",
		MarkerStyle:     "none",
		EnclosureStyle:  "angle",
	}

	input := "emailfoo/src/app.go and paren (foo/src/app.go:2)"
	got := TransformLocalReferences(input, cfg, "codex", "feishu", "/root")
	if strings.Contains(got, "email<app.go>") || strings.Contains(got, "emailfoo/<app.go>") {
		t.Fatalf("TransformLocalReferences() = %q, relative path after word should be preserved", got)
	}
	if !strings.Contains(got, "paren (<app.go:2>)") {
		t.Fatalf("TransformLocalReferences() = %q, relative path after punctuation should render", got)
	}

	disabled := TransformLocalReferences(input, cfg, "gemini", "feishu", "/root")
	if disabled != input {
		t.Fatalf("TransformLocalReferences() with unsupported agent = %q, want unchanged %q", disabled, input)
	}
}

func TestParseUserLocalReference_InvalidAndFileURLInputs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file URL path expectations assume Unix separators")
	}
	workspace := t.TempDir()
	filePath := filepath.Join(workspace, "docs", "guide.md")
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filePath, []byte("# guide\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	invalid := []string{"", "   ", "https://example.com/a.go", "//server/share/a.go", "plainword"}
	for _, raw := range invalid {
		t.Run(raw, func(t *testing.T) {
			if _, err := parseUserLocalReference(raw, workspace); err == nil {
				t.Fatalf("parseUserLocalReference(%q) returned nil error", raw)
			}
		})
	}

	ref, err := parseUserLocalReference("file://"+filePath+"#L3C2", workspace)
	if err != nil {
		t.Fatalf("parseUserLocalReference(file URL) error: %v", err)
	}
	if ref.kind != referenceKindFile || ref.locationFormat != referenceLocationHashLineCol {
		t.Fatalf("ref = %+v, want file with hash line/col", ref)
	}
	if ref.pathRel != "docs/guide.md" || ref.lineStart != 3 || ref.column != 2 {
		t.Fatalf("ref = %+v, want relative path and location", ref)
	}
}

func TestReferenceRenderingHelpers_DisplayModesAndStyles(t *testing.T) {
	ref := &localReference{
		kind:           referenceKindDir,
		pathOriginal:   "../outside/pkg",
		pathAbs:        "/tmp/workspace/pkg",
		pathRel:        "../pkg",
		isRelative:     true,
		locationFormat: referenceLocationColonLineCol,
		lineStart:      12,
		column:         4,
	}

	if got := referenceDisplaySource(ref, "absolute"); got != "/tmp/workspace/pkg/" {
		t.Fatalf("absolute display = %q", got)
	}
	if got := referenceDisplaySource(ref, "relative"); got != "../outside/pkg/" {
		t.Fatalf("relative fallback display = %q", got)
	}
	if got := renderReferenceLocation(ref); got != ":12:4" {
		t.Fatalf("renderReferenceLocation() = %q", got)
	}

	cases := []struct {
		style string
		body  string
		want  string
	}{
		{style: "bracket", body: "x", want: "[x]"},
		{style: "angle", body: "x", want: "<x>"},
		{style: "fullwidth", body: "x", want: "【x】"},
		{style: "code", body: "x", want: "`x`"},
		{style: "none", body: "x", want: "x"},
	}
	for _, tc := range cases {
		t.Run(tc.style, func(t *testing.T) {
			if got := applyReferenceEnclosure(tc.style, tc.body); got != tc.want {
				t.Fatalf("applyReferenceEnclosure(%q) = %q, want %q", tc.style, got, tc.want)
			}
		})
	}

	if got := applyReferenceMarker("ascii", referenceKindDir, "docs/"); got != "[DIR] docs/" {
		t.Fatalf("dir ascii marker = %q", got)
	}
	if got := applyReferenceMarker("ascii", referenceKindFile, "app.go"); got != "[FILE] app.go" {
		t.Fatalf("file ascii marker = %q", got)
	}
	if got := applyReferenceMarker("emoji", referenceKindUnknown, "mystery"); got != "mystery" {
		t.Fatalf("unknown emoji marker = %q", got)
	}
}
