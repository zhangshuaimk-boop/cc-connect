package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIsNewer(t *testing.T) {
	tests := []struct {
		latest, current string
		want            bool
	}{
		// Basic semver
		{"v1.2.3", "v1.2.2", true},
		{"v1.2.2", "v1.2.3", false},
		{"v1.2.3", "v1.2.3", false},
		{"v2.0.0", "v1.9.9", true},

		// Pre-release vs stable
		{"v1.2.3", "v1.2.3-beta.1", true},
		{"v1.2.3-beta.1", "v1.2.3", false},

		// Pre-release numeric ordering
		{"v1.2.3-beta.10", "v1.2.3-beta.2", true},
		{"v1.2.3-beta.2", "v1.2.3-beta.10", false},
		{"v1.2.3-beta.2", "v1.2.3-beta.2", false},

		// rc > beta lexicographically
		{"v1.2.3-rc.1", "v1.2.3-beta.9", true},

		// Dev builds always upgradeable
		{"v1.0.0", "dev", true},

		// Empty
		{"", "v1.0.0", false},
		{"v1.0.0", "", false},
	}
	for _, tt := range tests {
		got := isNewer(tt.latest, tt.current)
		if got != tt.want {
			t.Errorf("isNewer(%q, %q) = %v, want %v", tt.latest, tt.current, got, tt.want)
		}
	}
}

func TestGetUpdateHintIfAvailable_NeverBlocks(t *testing.T) {
	origVersion := version
	defer func() { version = origVersion }()
	version = "v1.0.0"

	// Clear cache to force cache miss
	cachedLatestVersion.mu.Lock()
	cachedLatestVersion.version = ""
	cachedLatestVersion.timestamp = time.Time{}
	cachedLatestVersion.mu.Unlock()

	// getUpdateHintIfAvailable should return "" immediately on cache miss
	// (async fetch is kicked off in background but does not block)
	start := time.Now()
	hint := getUpdateHintIfAvailable()
	elapsed := time.Since(start)

	if hint != "" {
		t.Errorf("expected empty hint on cache miss, got: %q", hint)
	}
	if elapsed > 2*time.Second {
		t.Errorf("getUpdateHintIfAvailable blocked for %v, should return immediately", elapsed)
	}
}

func TestGetUpdateHintIfAvailable_UsesCache(t *testing.T) {
	origVersion := version
	defer func() { version = origVersion }()
	version = "v1.0.0"

	// Populate cache with a newer version
	cachedLatestVersion.mu.Lock()
	cachedLatestVersion.version = "v2.0.0"
	cachedLatestVersion.timestamp = time.Now()
	cachedLatestVersion.mu.Unlock()

	hint := getUpdateHintIfAvailable()
	if hint == "" {
		t.Error("expected update hint when cache has newer version")
	}

	// Populate cache with same version — should return empty
	cachedLatestVersion.mu.Lock()
	cachedLatestVersion.version = "v1.0.0"
	cachedLatestVersion.timestamp = time.Now()
	cachedLatestVersion.mu.Unlock()

	hint = getUpdateHintIfAvailable()
	if hint != "" {
		t.Errorf("expected no hint when versions match, got: %q", hint)
	}
}

func TestGetUpdateHintIfAvailable_DevSkipped(t *testing.T) {
	origVersion := version
	defer func() { version = origVersion }()
	version = "dev"

	hint := getUpdateHintIfAvailable()
	if hint != "" {
		t.Errorf("expected empty hint for dev version, got: %q", hint)
	}
}

func TestSyncNpmPackageVersion_NormalizesVPrefix(t *testing.T) {
	// Regression test: old package.json stored version as "v1.0.0" but newVer
	// is already stripped to "1.0.0". They should be treated as equal.
	dir := t.TempDir()
	ccConnectDir := filepath.Join(dir, "node_modules", "cc-connect")
	binDir := filepath.Join(ccConnectDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	execPath := filepath.Join(binDir, "cc-connect")

	pkgJSON := filepath.Join(ccConnectDir, "package.json")
	pkgData := `{"name": "cc-connect", "version": "v1.0.0"}`
	if err := os.WriteFile(pkgJSON, []byte(pkgData), 0o644); err != nil {
		t.Fatalf("write pkg.json: %v", err)
	}

	// newVer has "v" already stripped: "1.0.0" vs package.json "v1.0.0"
	syncNpmPackageVersion(execPath, "1.0.0")

	// Re-read and verify version was NOT overwritten (same version)
	content, err := os.ReadFile(pkgJSON)
	if err != nil {
		t.Fatalf("read pkg.json: %v", err)
	}
	var pkg map[string]any
	if err := json.Unmarshal(content, &pkg); err != nil {
		t.Fatalf("parse pkg.json: %v", err)
	}
	// Version should still be "v1.0.0" (not overwritten with "1.0.0")
	if pkg["version"] != "v1.0.0" {
		t.Errorf("version = %v, want v1.0.0 (unchanged)", pkg["version"])
	}
}

func TestSyncNpmPackageVersion_UpdatesWhenDifferent(t *testing.T) {
	dir := t.TempDir()
	ccConnectDir := filepath.Join(dir, "node_modules", "cc-connect")
	binDir := filepath.Join(ccConnectDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	execPath := filepath.Join(binDir, "cc-connect")

	pkgJSON := filepath.Join(ccConnectDir, "package.json")
	pkgData := `{"name": "cc-connect", "version": "v0.9.0"}`
	if err := os.WriteFile(pkgJSON, []byte(pkgData), 0o644); err != nil {
		t.Fatalf("write pkg.json: %v", err)
	}

	syncNpmPackageVersion(execPath, "1.0.0")

	content, err := os.ReadFile(pkgJSON)
	if err != nil {
		t.Fatalf("read pkg.json: %v", err)
	}
	var pkg map[string]any
	if err := json.Unmarshal(content, &pkg); err != nil {
		t.Fatalf("parse pkg.json: %v", err)
	}
	if pkg["version"] != "1.0.0" {
		t.Errorf("version = %v, want 1.0.0 (updated)", pkg["version"])
	}
}

func TestFetchLatestStableRelease_FallsBackToRedirect(t *testing.T) {
	transport := http.DefaultTransport
	defer func() { http.DefaultTransport = transport }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/chenhg5/cc-connect/releases/latest":
			http.Error(w, "api unavailable", http.StatusInternalServerError)
		case "/chenhg5/cc-connect/releases/latest":
			w.Header().Set("Location", serverURL(t, r)+"/chenhg5/cc-connect/releases/tag/v9.8.7")
			w.WriteHeader(http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	http.DefaultTransport = rewriteHostTransport{base: server.URL, rt: transport}

	release, err := fetchLatestStableRelease()
	if err != nil {
		t.Fatalf("fetchLatestStableRelease returned error: %v", err)
	}
	if release.TagName != "v9.8.7" || release.HTMLURL == "" {
		t.Fatalf("release = %+v, want redirect tag", release)
	}
}

func TestFetchLatestPreReleaseAndGiteeBranches(t *testing.T) {
	transport := http.DefaultTransport
	defer func() { http.DefaultTransport = transport }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/chenhg5/cc-connect/releases":
			if r.URL.Query().Get("per_page") != "10" {
				t.Fatalf("per_page query = %q, want 10", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode([]githubRelease{
				{TagName: "v2.0.0-beta.1", Prerelease: true},
				{TagName: "v1.9.0"},
			})
		case "/api/v5/repos/cg33/cc-connect/releases/latest":
			_ = json.NewEncoder(w).Encode(githubRelease{TagName: "v1.0.0", Prerelease: true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	http.DefaultTransport = rewriteHostTransport{base: server.URL, rt: transport}

	release, err := fetchLatestPreRelease()
	if err != nil {
		t.Fatalf("fetchLatestPreRelease returned error: %v", err)
	}
	if release.TagName != "v2.0.0-beta.1" || !release.Prerelease {
		t.Fatalf("pre release = %+v, want newest pre-release", release)
	}

	giteeRelease, err := fetchLatestStableFromGitee()
	if err != nil {
		t.Fatalf("fetchLatestStableFromGitee returned error: %v", err)
	}
	if giteeRelease != nil {
		t.Fatalf("gitee pre-release = %+v, want nil stable release", giteeRelease)
	}
}

func TestDownloadExtractAndReplaceExecutableBranches(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/binary":
			_, _ = w.Write([]byte("new-binary"))
		case "/missing":
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	tmp, err := downloadToTemp(server.URL + "/binary")
	if err != nil {
		t.Fatalf("downloadToTemp returned error: %v", err)
	}
	defer os.Remove(tmp)
	data, err := os.ReadFile(tmp)
	if err != nil {
		t.Fatalf("read downloaded temp: %v", err)
	}
	if string(data) != "new-binary" {
		t.Fatalf("downloaded data = %q, want new-binary", data)
	}

	if _, err := downloadToTemp(server.URL + "/missing"); err == nil || !strings.Contains(err.Error(), "HTTP 404") {
		t.Fatalf("downloadToTemp missing error = %v, want HTTP 404", err)
	}

	tarPath := filepath.Join(t.TempDir(), "cc-connect.tar.gz")
	writeTestTarGz(t, tarPath, "cc-connect", "tar-binary")
	extracted, err := extractBinaryFromArchive(tarPath, "cc-connect.tar.gz")
	if err != nil {
		t.Fatalf("extract tar: %v", err)
	}
	defer os.Remove(extracted)
	if got, _ := os.ReadFile(extracted); string(got) != "tar-binary" {
		t.Fatalf("tar extracted data = %q", got)
	}

	zipPath := filepath.Join(t.TempDir(), "cc-connect.zip")
	writeTestZip(t, zipPath, "cc-connect.exe", "zip-binary")
	extractedZip, err := extractBinaryFromArchive(zipPath, "cc-connect.zip")
	if err != nil {
		t.Fatalf("extract zip: %v", err)
	}
	defer os.Remove(extractedZip)
	if got, _ := os.ReadFile(extractedZip); string(got) != "zip-binary" {
		t.Fatalf("zip extracted data = %q", got)
	}

	dir := t.TempDir()
	target := filepath.Join(dir, "cc-connect")
	src := filepath.Join(dir, "new")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if err := os.WriteFile(src, []byte("new"), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	if err := replaceExecutable(target, src); err != nil {
		t.Fatalf("replaceExecutable returned error: %v", err)
	}
	if got, _ := os.ReadFile(target); string(got) != "new" {
		t.Fatalf("target data = %q, want new", got)
	}
	if _, err := os.Stat(target + ".old"); !os.IsNotExist(err) {
		t.Fatalf("backup still exists or stat error = %v", err)
	}
}

func TestUpdateHelpersAssetNamesAndCopyErrors(t *testing.T) {
	if got := archiveAssetName("v1.2.3"); !strings.Contains(got, "cc-connect-v1.2.3-") {
		t.Fatalf("archiveAssetName = %q, want tag and platform", got)
	}
	if got := binaryAssetName("v1.2.3"); !strings.Contains(got, "cc-connect-v1.2.3-") {
		t.Fatalf("binaryAssetName = %q, want tag and platform", got)
	}

	dir := t.TempDir()
	dst := filepath.Join(dir, "dst")
	if err := copyFile(filepath.Join(dir, "missing"), dst); err == nil {
		t.Fatal("copyFile missing source returned nil error")
	}
	if _, err := extractBinaryFromArchive(filepath.Join(dir, "missing.tar.gz"), "missing.tar.gz"); err == nil {
		t.Fatal("extractBinaryFromArchive missing file returned nil error")
	}
}

type rewriteHostTransport struct {
	base string
	rt   http.RoundTripper
}

func (t rewriteHostTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	baseReq, err := http.NewRequest(req.Method, t.base+req.URL.RequestURI(), req.Body)
	if err != nil {
		return nil, err
	}
	baseReq.Header = req.Header.Clone()
	return t.rt.RoundTrip(baseReq)
}

func serverURL(t *testing.T, r *http.Request) string {
	t.Helper()
	if r.TLS != nil {
		return "https://" + r.Host
	}
	return "http://" + r.Host
}

func writeTestTarGz(t *testing.T, path, name, content string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create tar.gz: %v", err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content))}); err != nil {
		t.Fatalf("write tar header: %v", err)
	}
	if _, err := tw.Write([]byte(content)); err != nil {
		t.Fatalf("write tar content: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close tar.gz file: %v", err)
	}
}

func writeTestZip(t *testing.T, path, name, content string) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatalf("create zip entry: %v", err)
	}
	if _, err := w.Write([]byte(content)); err != nil {
		t.Fatalf("write zip content: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write zip file: %v", err)
	}
}
