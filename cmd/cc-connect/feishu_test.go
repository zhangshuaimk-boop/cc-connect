package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chenhg5/cc-connect/config"
)

func TestResolveFeishuSetupInputs_AutoModeWithoutCredentialsUsesNew(t *testing.T) {
	mode, appID, appSecret, err := resolveFeishuSetupInputs(feishuSetupModeAuto, "", "", "")
	if err != nil {
		t.Fatalf("resolveFeishuSetupInputs returned error: %v", err)
	}
	if mode != feishuSetupModeNew {
		t.Fatalf("mode = %q, want %q", mode, feishuSetupModeNew)
	}
	if appID != "" || appSecret != "" {
		t.Fatalf("credentials should be empty, got appID=%q appSecret=%q", appID, appSecret)
	}
}

func TestResolveFeishuSetupInputs_AutoModeWithAppUsesBind(t *testing.T) {
	mode, appID, appSecret, err := resolveFeishuSetupInputs(feishuSetupModeAuto, "cli_xxx:sec_xxx", "", "")
	if err != nil {
		t.Fatalf("resolveFeishuSetupInputs returned error: %v", err)
	}
	if mode != feishuSetupModeBind {
		t.Fatalf("mode = %q, want %q", mode, feishuSetupModeBind)
	}
	if appID != "cli_xxx" || appSecret != "sec_xxx" {
		t.Fatalf("credentials = (%q, %q), want (%q, %q)", appID, appSecret, "cli_xxx", "sec_xxx")
	}
}

func TestResolveFeishuSetupInputs_BindRequiresCredentials(t *testing.T) {
	_, _, _, err := resolveFeishuSetupInputs(feishuSetupModeBind, "", "", "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestResolveFeishuSetupInputs_RejectsMixedCredentialFlags(t *testing.T) {
	_, _, _, err := resolveFeishuSetupInputs(feishuSetupModeAuto, "cli_xxx:sec_xxx", "cli_xxx", "sec_xxx")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestParseAppPair_SecretCanContainColon(t *testing.T) {
	appID, appSecret, err := parseAppPair("cli_xxx:sec:yyy")
	if err != nil {
		t.Fatalf("parseAppPair returned error: %v", err)
	}
	if appID != "cli_xxx" || appSecret != "sec:yyy" {
		t.Fatalf("result = (%q, %q), want (%q, %q)", appID, appSecret, "cli_xxx", "sec:yyy")
	}
}

func TestParseAppPair_TrimsBothPartsAndRejectsInvalidFormats(t *testing.T) {
	appID, appSecret, err := parseAppPair("  cli_trim  :  sec_trim  ")
	if err != nil {
		t.Fatalf("parseAppPair returned error: %v", err)
	}
	if appID != "cli_trim" || appSecret != "sec_trim" {
		t.Fatalf("result = (%q, %q), want trimmed credentials", appID, appSecret)
	}

	for _, raw := range []string{"", "cli_only", ":secret", "cli:", "  :  "} {
		t.Run(fmt.Sprintf("%q", raw), func(t *testing.T) {
			if _, _, err := parseAppPair(raw); err == nil {
				t.Fatalf("parseAppPair(%q) returned nil error", raw)
			}
		})
	}
}

func TestNormalizeFeishuPlatformType(t *testing.T) {
	cases := []struct {
		raw     string
		want    string
		wantErr bool
	}{
		{raw: "", want: ""},
		{raw: "  FEISHU ", want: "feishu"},
		{raw: "Lark", want: "lark"},
		{raw: "slack", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			got, err := normalizeFeishuPlatformType(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("normalizeFeishuPlatformType(%q) returned nil error", tc.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeFeishuPlatformType(%q) returned error: %v", tc.raw, err)
			}
			if got != tc.want {
				t.Fatalf("normalizeFeishuPlatformType(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestContainsString_TrimsAndIgnoresCase(t *testing.T) {
	values := []string{" authorization_pending ", "CLIENT_SECRET"}

	if !containsString(values, "client_secret") {
		t.Fatal("containsString should match with trim and case folding")
	}
	if containsString(values, "password") {
		t.Fatal("containsString matched missing value")
	}
}

func TestValidateAppCredentialsAgainstBase(t *testing.T) {
	tests := []struct {
		name    string
		body    any
		wantOK  bool
		wantErr string
	}{
		{
			name:   "valid token response",
			body:   tenantTokenResponse{Code: 0, TenantAccessToken: "tenant-token"},
			wantOK: true,
		},
		{
			name:    "remote error message",
			body:    tenantTokenResponse{Code: 999, Msg: "bad credentials"},
			wantErr: "code=999 msg=bad credentials",
		},
		{
			name:   "non-zero without message is invalid without hard error",
			body:   tenantTokenResponse{Code: 999},
			wantOK: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var gotPath, gotContentType string
			var gotPayload map[string]string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				gotContentType = r.Header.Get("Content-Type")
				if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
					t.Fatalf("decode request body: %v", err)
				}
				_ = json.NewEncoder(w).Encode(tc.body)
			}))
			defer server.Close()

			gotOK, err := validateAppCredentialsAgainstBase(server.URL, "cli_test", "sec_test")

			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("validateAppCredentialsAgainstBase returned error: %v", err)
			}
			if gotOK != tc.wantOK {
				t.Fatalf("ok = %v, want %v", gotOK, tc.wantOK)
			}
			if gotPath != "/open-apis/auth/v3/tenant_access_token/internal" {
				t.Fatalf("path = %q", gotPath)
			}
			if gotContentType != "application/json" {
				t.Fatalf("content type = %q", gotContentType)
			}
			if gotPayload["app_id"] != "cli_test" || gotPayload["app_secret"] != "sec_test" {
				t.Fatalf("payload = %#v", gotPayload)
			}
		})
	}
}

func TestValidateAppCredentialsAgainstBase_RejectsMalformedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{not-json"))
	}))
	defer server.Close()

	ok, err := validateAppCredentialsAgainstBase(server.URL, "cli_test", "sec_test")
	if err == nil || !strings.Contains(err.Error(), "decode response") {
		t.Fatalf("error = %v, want decode response", err)
	}
	if ok {
		t.Fatal("ok = true, want false")
	}
}

func TestSaveQRCodeImage_CreatesPNG(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test-qr.png")

	if err := saveQRCodeImage("https://example.com/test", path); err != nil {
		t.Fatalf("saveQRCodeImage failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read output file: %v", err)
	}
	if len(data) < 100 {
		t.Fatalf("PNG file too small: %d bytes", len(data))
	}
	// PNG magic bytes
	if data[0] != 0x89 || data[1] != 'P' || data[2] != 'N' || data[3] != 'G' {
		t.Fatal("output file is not a valid PNG")
	}
}

func TestSaveQRCodeImage_InvalidPath(t *testing.T) {
	err := saveQRCodeImage("https://example.com", "/nonexistent/dir/qr.png")
	if err == nil {
		t.Fatal("expected error for invalid path, got nil")
	}
}

func TestRunFeishuUsageAndErrorBranches(t *testing.T) {
	stdout, stderr := captureCronTimerOutput(t, func() {
		runFeishu(nil)
	})
	if stderr != "" {
		t.Fatalf("runFeishu no args stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, "Usage: cc-connect feishu") || !strings.Contains(stdout, "setup") {
		t.Fatalf("runFeishu no args stdout missing usage:\n%s", stdout)
	}

	unknown := runFeishuCommandHelper(t, "bogus")
	if unknown.code == 0 || !strings.Contains(unknown.stderr, "Unknown feishu subcommand: bogus") || !strings.Contains(unknown.stdout, "Usage: cc-connect feishu") {
		t.Fatalf("unknown feishu command result = %+v, want usage failure", unknown)
	}

	missingApp := runFeishuCommandHelper(t, "bind", "--project", "demo")
	if missingApp.code == 0 || !strings.Contains(missingApp.stderr, "bind mode requires credentials") {
		t.Fatalf("missing app result = %+v, want credentials failure", missingApp)
	}

	invalidPlatform := runFeishuCommandHelper(t, "setup", "--project", "demo", "--platform-type", "slack")
	if invalidPlatform.code == 0 || !strings.Contains(invalidPlatform.stderr, "invalid --platform-type") {
		t.Fatalf("invalid platform result = %+v, want platform validation failure", invalidPlatform)
	}
}

func TestRunFeishuSetup_MissingProjectConfigFailsBeforeNetwork(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte("language = \"en\"\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	result := runFeishuCommandHelper(t, "setup", "--config", configPath)
	if result.code == 0 || !strings.Contains(result.stderr, "no project found in config") {
		t.Fatalf("feishu setup empty config result = %+v, want no project failure", result)
	}
}

func TestResolveTargetProject_ConfigSelection(t *testing.T) {
	origPath := config.ConfigPath
	t.Cleanup(func() { config.ConfigPath = origPath })

	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte(`
[[projects]]
name = "beta"

[[projects.platforms]]
type = "feishu"

[[projects]]
name = "alpha"

[[projects.platforms]]
type = "lark"
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	config.ConfigPath = configPath

	got, err := resolveTargetProject(" explicit ")
	if err != nil {
		t.Fatalf("resolve explicit project returned error: %v", err)
	}
	if got != "explicit" {
		t.Fatalf("explicit project = %q, want trimmed value", got)
	}

	_, err = resolveTargetProject("")
	if err == nil || !strings.Contains(err.Error(), "multiple projects found") || !strings.Contains(err.Error(), "alpha, beta") {
		t.Fatalf("resolve multiple projects error = %v, want sorted ambiguity", err)
	}

	oneProject := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(oneProject, []byte(`
[[projects]]
name = "only"

[[projects.platforms]]
type = "feishu"
`), 0o644); err != nil {
		t.Fatalf("write one project config: %v", err)
	}
	config.ConfigPath = oneProject
	got, err = resolveTargetProject("")
	if err != nil {
		t.Fatalf("resolve one project returned error: %v", err)
	}
	if got != "only" {
		t.Fatalf("single project = %q, want only", got)
	}
}

func TestFeishuGuidanceOutputBranches(t *testing.T) {
	stdout, stderr := captureCronTimerOutput(t, func() {
		printBotMenuGuidance("lark")
	})
	if stderr != "" {
		t.Fatalf("printBotMenuGuidance stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, "https://open.larksuite.com/app") || !strings.Contains(stdout, "/whoami") {
		t.Fatalf("lark guidance output missing expected content:\n%s", stdout)
	}

	stdout, stderr = captureCronTimerOutput(t, func() {
		printAllowFromGuidance("bad-app", "bad-secret", "ou_owner", &config.FeishuCredentialUpdateResult{
			PlatformType: "feishu",
			AllowFrom:    "",
		})
	})
	if stderr != "" {
		t.Fatalf("printAllowFromGuidance stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, "allow_from") || !strings.Contains(stdout, "/whoami") {
		t.Fatalf("allow_from guidance output missing expected content:\n%s", stdout)
	}
}

type p9FeishuCommandResult struct {
	stdout string
	stderr string
	code   int
}

func runFeishuCommandHelper(t *testing.T, args ...string) p9FeishuCommandResult {
	t.Helper()
	cmdArgs := append([]string{"-test.run=TestP9FeishuCommandHelperProcess", "--"}, args...)
	cmd := exec.Command(os.Args[0], cmdArgs...)
	cmd.Env = append(os.Environ(), "CC_CONNECT_P9_FEISHU_HELPER=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := p9FeishuCommandResult{stdout: stdout.String(), stderr: stderr.String()}
	if exitErr, ok := err.(*exec.ExitError); ok {
		result.code = exitErr.ExitCode()
		return result
	}
	if err != nil {
		t.Fatalf("run feishu helper command: %v", err)
	}
	return result
}

func TestP9FeishuCommandHelperProcess(t *testing.T) {
	if os.Getenv("CC_CONNECT_P9_FEISHU_HELPER") != "1" {
		return
	}
	idx := -1
	for i, arg := range os.Args {
		if arg == "--" {
			idx = i
			break
		}
	}
	if idx < 0 {
		os.Exit(2)
	}
	runFeishu(os.Args[idx+1:])
	os.Exit(0)
}
