package main

import (
	"bytes"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/chenhg5/cc-connect/config"
)

type commandResult struct {
	stdout string
	stderr string
	code   int
}

func TestProviderCommands_AddListRemove(t *testing.T) {
	configPath := writeProviderCommandConfig(t, `
[[projects]]
name = "demo"

[projects.agent]
type = "claudecode"

[projects.agent.options]
provider = "primary"

[[projects.agent.providers]]
name = "primary"
api_key = "sk-primary-123456"
base_url = "https://primary.example/v1"
model = "claude-primary"

[[projects.platforms]]
type = "feishu"
`)

	stdout, stderr := captureCommandOutput(t, func() {
		runProviderAdd([]string{
			"--config", configPath,
			"--project", "demo",
			"--name", "relay",
			"--api-key", "sk-relay-abcdef",
			"--base-url", "https://relay.example/v1",
			"--model", "claude-relay",
			"--env", "HTTP_PROXY=http://127.0.0.1:7890,EMPTY,HTTPS_PROXY=https://127.0.0.1:7890",
		})
	})
	if stderr != "" {
		t.Fatalf("runProviderAdd stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, `Provider "relay" added to project "demo"`) {
		t.Fatalf("runProviderAdd stdout missing success message:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Base URL: https://relay.example/v1") {
		t.Fatalf("runProviderAdd stdout missing base URL:\n%s", stdout)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("Load updated config: %v", err)
	}
	if len(cfg.Projects[0].Agent.Providers) != 2 {
		t.Fatalf("provider count = %d, want 2", len(cfg.Projects[0].Agent.Providers))
	}
	added := cfg.Projects[0].Agent.Providers[1]
	if added.Name != "relay" || added.APIKey != "sk-relay-abcdef" || added.BaseURL != "https://relay.example/v1" || added.Model != "claude-relay" {
		t.Fatalf("added provider = %+v, want relay fields", added)
	}
	if added.Env["HTTP_PROXY"] != "http://127.0.0.1:7890" || added.Env["HTTPS_PROXY"] != "https://127.0.0.1:7890" {
		t.Fatalf("added provider env = %+v", added.Env)
	}
	if _, ok := added.Env["EMPTY"]; ok {
		t.Fatalf("parseEnvStr kept malformed env entry: %+v", added.Env)
	}

	stdout, stderr = captureCommandOutput(t, func() {
		runProviderList([]string{"--config", configPath, "--project", "demo"})
	})
	if stderr != "" {
		t.Fatalf("runProviderList stderr = %q, want empty", stderr)
	}
	for _, want := range []string{
		"primary (base_url: https://primary.example/v1) [model: claude-primary]  api_key: sk-p...3456",
		"relay (base_url: https://relay.example/v1) [model: claude-relay]  api_key: sk-r...cdef",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("runProviderList stdout missing %q:\n%s", want, stdout)
		}
	}
	if !strings.Contains(stdout, "\u25b6 primary") {
		t.Fatalf("runProviderList stdout missing active provider marker:\n%s", stdout)
	}

	stdout, stderr = captureCommandOutput(t, func() {
		runProviderRemove([]string{"--config", configPath, "--project", "demo", "--name", "relay"})
	})
	if stderr != "" {
		t.Fatalf("runProviderRemove stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, `Provider "relay" removed from project "demo"`) {
		t.Fatalf("runProviderRemove stdout missing success message:\n%s", stdout)
	}
	cfg, err = config.Load(configPath)
	if err != nil {
		t.Fatalf("Load after remove: %v", err)
	}
	if len(cfg.Projects[0].Agent.Providers) != 1 || cfg.Projects[0].Agent.Providers[0].Name != "primary" {
		t.Fatalf("providers after remove = %+v, want only primary", cfg.Projects[0].Agent.Providers)
	}
}

func TestProviderCommands_ListAllProjectsAndEmptyProject(t *testing.T) {
	configPath := writeProviderCommandConfig(t, `
[[projects]]
name = "alpha"

[projects.agent]
type = "claudecode"

[projects.agent.options]
provider = "primary"

[[projects.agent.providers]]
name = "primary"
api_key = "short"

[[projects]]
name = "beta"

[projects.agent]
type = "codex"
`)

	stdout, stderr := captureCommandOutput(t, func() {
		runProviderList([]string{"--config", configPath})
	})
	if stderr != "" {
		t.Fatalf("runProviderList stderr = %q, want empty", stderr)
	}
	for _, want := range []string{"\u2500\u2500 alpha \u2500\u2500", "\u25b6 primary  api_key: ****", "\u2500\u2500 beta \u2500\u2500", "(no providers)"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("runProviderList stdout missing %q:\n%s", want, stdout)
		}
	}
}

func TestProviderCommands_GlobalAddListRemove(t *testing.T) {
	configPath := writeProviderCommandConfig(t, `
[[projects]]
name = "demo"

[projects.agent]
type = "claudecode"

[[projects.platforms]]
type = "feishu"
`)

	stdout, stderr := captureCommandOutput(t, func() {
		runGlobalProviderAdd([]string{
			"--config", configPath,
			"--name", "global-relay",
			"--api-key", "sk-global-abcdef",
			"--base-url", "https://global.example/v1",
			"--model", "gpt-global",
			"--thinking", "disabled",
			"--env", "NO_PROXY=localhost",
		})
	})
	if stderr != "" {
		t.Fatalf("runGlobalProviderAdd stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, `Global provider "global-relay" added`) {
		t.Fatalf("runGlobalProviderAdd stdout missing success message:\n%s", stdout)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("Load updated config: %v", err)
	}
	if len(cfg.Providers) != 1 {
		t.Fatalf("global provider count = %d, want 1", len(cfg.Providers))
	}
	if got := cfg.Providers[0]; got.Name != "global-relay" || got.Thinking != "disabled" || got.Env["NO_PROXY"] != "localhost" {
		t.Fatalf("global provider = %+v, want configured provider", got)
	}

	stdout, stderr = captureCommandOutput(t, func() {
		runGlobalProviderList([]string{"--config", configPath})
	})
	if stderr != "" {
		t.Fatalf("runGlobalProviderList stderr = %q, want empty", stderr)
	}
	for _, want := range []string{"Global Providers (1)", "global-relay (https://global.example/v1) [gpt-global]", "sk-g...cdef"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("runGlobalProviderList stdout missing %q:\n%s", want, stdout)
		}
	}

	stdout, stderr = captureCommandOutput(t, func() {
		runGlobalProviderRemove([]string{"--config", configPath, "--name", "global-relay"})
	})
	if stderr != "" {
		t.Fatalf("runGlobalProviderRemove stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, `Global provider "global-relay" removed`) {
		t.Fatalf("runGlobalProviderRemove stdout missing success message:\n%s", stdout)
	}
	cfg, err = config.Load(configPath)
	if err != nil {
		t.Fatalf("Load after remove: %v", err)
	}
	if len(cfg.Providers) != 0 {
		t.Fatalf("global providers after remove = %+v, want empty", cfg.Providers)
	}
}

func TestProviderCommands_GlobalListEmpty(t *testing.T) {
	configPath := writeProviderCommandConfig(t, `
[[projects]]
name = "demo"

[projects.agent]
type = "claudecode"

[[projects.platforms]]
type = "feishu"
`)

	stdout, stderr := captureCommandOutput(t, func() {
		runGlobalProviderList([]string{"--config", configPath})
	})
	if stderr != "" {
		t.Fatalf("runGlobalProviderList stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, "No global providers configured.") {
		t.Fatalf("runGlobalProviderList stdout missing empty message:\n%s", stdout)
	}
}

func TestProviderImportCommand_FromSQLiteDatabase(t *testing.T) {
	configPath := writeProviderCommandConfig(t, `
[[projects]]
name = "demo"

[projects.agent]
type = "claudecode"

[[projects.platforms]]
type = "feishu"
`)
	dbPath := writeCCSwitchProviderDB(t)

	stdout, stderr := captureCommandOutput(t, func() {
		runProviderImport([]string{
			"--config", configPath,
			"--project", "demo",
			"--db-path", dbPath,
			"--type", "claude",
		})
	})
	if stderr != "" {
		t.Fatalf("runProviderImport stderr = %q, want empty", stderr)
	}
	for _, want := range []string{
		"Importing from: " + dbPath,
		"Target project: demo",
		"Claude Relay [claude] \u2192 claude-relay (was active in cc-switch)",
		"Done: 1 imported, 0 skipped",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("runProviderImport stdout missing %q:\n%s", want, stdout)
		}
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("Load after import: %v", err)
	}
	if len(cfg.Projects[0].Agent.Providers) != 1 {
		t.Fatalf("provider count after import = %d, want 1", len(cfg.Projects[0].Agent.Providers))
	}
	imported := cfg.Projects[0].Agent.Providers[0]
	if imported.Name != "claude-relay" || imported.APIKey != "sk-claude" || imported.BaseURL != "https://claude.example" || imported.Model != "opus" {
		t.Fatalf("imported provider = %+v", imported)
	}
}

func TestProviderAddCommand_WriteFailureFromReadOnlyDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod read-only directory semantics differ on Windows")
	}

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(configPath, []byte(`
[[projects]]
name = "demo"

[projects.agent]
type = "claudecode"

[[projects.platforms]]
type = "feishu"
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("chmod read-only dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(dir, 0o755)
	})

	result := runCCConnectCommandHelper(t, "provider", "add", "--config", configPath, "--project", "demo", "--name", "relay")
	if result.code == 0 {
		t.Fatalf("provider add exited with code 0, want failure; stdout=%q stderr=%q", result.stdout, result.stderr)
	}
	if !strings.Contains(result.stderr, "create temp config:") {
		t.Fatalf("stderr missing write failure:\n%s", result.stderr)
	}
}

func TestProviderCommandHelpersAndImportConversion(t *testing.T) {
	if got := parseEnvStr("A=1, B=two ,missing, C=three=four,=bad"); got["A"] != "1" || got["B"] != "two" || got["C"] != "three=four" || len(got) != 3 {
		t.Fatalf("parseEnvStr() = %+v", got)
	}

	baseURL, model := parseCodexConfigTOML(`
model = "gpt-5"
base_url = "https://api.example/v1"
`)
	if baseURL != "https://api.example/v1" || model != "gpt-5" {
		t.Fatalf("parseCodexConfigTOML() = (%q, %q)", baseURL, model)
	}
	if key, value, ok := parseTOMLKV(`# model = "ignored"`); ok || key != "" || value != "" {
		t.Fatalf("parseTOMLKV(comment) = (%q, %q, %v), want empty false", key, value, ok)
	}

	claude, err := convertCCSwitchProvider(ccSwitchRow{
		AppType:        "claude",
		Name:           "My Claude",
		SettingsConfig: `{"env":{"ANTHROPIC_AUTH_TOKEN":"sk-claude","ANTHROPIC_BASE_URL":"https://claude.example","ANTHROPIC_MODEL":"opus","EXTRA":"1"}}`,
	})
	if err != nil {
		t.Fatalf("convertCCSwitchProvider(claude) error: %v", err)
	}
	if claude.Name != "my-claude" || claude.APIKey != "sk-claude" || claude.BaseURL != "https://claude.example" || claude.Model != "opus" || claude.Env["EXTRA"] != "1" {
		t.Fatalf("claude provider = %+v", claude)
	}

	codex, err := convertCCSwitchProvider(ccSwitchRow{
		AppType:        "codex",
		Name:           "Codex",
		SettingsConfig: `{"auth":{"OPENAI_API_KEY":"sk-openai"},"config":"model = \"gpt-5\"\nbase_url = \"https://openai.example/v1\""}`,
	})
	if err != nil {
		t.Fatalf("convertCCSwitchProvider(codex) error: %v", err)
	}
	if codex.Name != "codex" || codex.APIKey != "sk-openai" || codex.BaseURL != "https://openai.example/v1" || codex.Model != "gpt-5" {
		t.Fatalf("codex provider = %+v", codex)
	}

	for _, row := range []ccSwitchRow{
		{AppType: "unknown", Name: "Bad", SettingsConfig: `{}`},
		{AppType: "claude", Name: "No Env", SettingsConfig: `{}`},
		{AppType: "codex", Name: "No Key", SettingsConfig: `{"auth":{}}`},
		{AppType: "claude", Name: "Bad JSON", SettingsConfig: `not-json`},
	} {
		if _, err := convertCCSwitchProvider(row); err == nil {
			t.Fatalf("convertCCSwitchProvider(%+v) returned nil error", row)
		}
	}
}

func TestProviderCommands_ExitFailures(t *testing.T) {
	configPath := writeProviderCommandConfig(t, `
[[projects]]
name = "demo"

[projects.agent]
type = "claudecode"
`)

	tests := []struct {
		name       string
		args       []string
		wantStderr string
		wantStdout string
	}{
		{
			name:       "missing root subcommand",
			args:       nil,
			wantStdout: "Usage: cc-connect provider <command>",
		},
		{
			name:       "unknown root subcommand",
			args:       []string{"wat"},
			wantStderr: "Unknown provider subcommand: wat",
			wantStdout: "Usage: cc-connect provider <command>",
		},
		{
			name:       "switch is not a CLI subcommand",
			args:       []string{"switch", "--project", "demo", "--name", "primary"},
			wantStderr: "Unknown provider subcommand: switch",
			wantStdout: "Usage: cc-connect provider <command>",
		},
		{
			name:       "add missing args",
			args:       []string{"add", "--config", configPath, "--project", "demo"},
			wantStderr: "Error: --project and --name are required",
		},
		{
			name:       "remove missing provider",
			args:       []string{"remove", "--config", configPath, "--project", "demo", "--name", "missing"},
			wantStderr: `provider "missing" not found in project "demo"`,
		},
		{
			name:       "add config write failure",
			args:       []string{"add", "--config", filepath.Join(t.TempDir(), "missing-dir", "config.toml"), "--project", "demo", "--name", "relay"},
			wantStderr: "read config:",
		},
		{
			name:       "global missing subcommand",
			args:       []string{"global"},
			wantStderr: "Usage: cc-connect provider global <command>",
		},
		{
			name:       "global unknown subcommand",
			args:       []string{"global", "wat"},
			wantStderr: "Unknown global subcommand: wat",
		},
		{
			name:       "global add missing name",
			args:       []string{"global", "add", "--config", configPath},
			wantStderr: "Error: --name is required",
		},
		{
			name:       "global remove missing provider",
			args:       []string{"global", "remove", "--config", configPath, "--name", "missing"},
			wantStderr: `global provider "missing" not found`,
		},
		{
			name:       "import missing database",
			args:       []string{"import", "--config", configPath, "--db-path", filepath.Join(t.TempDir(), "missing.sqlite")},
			wantStderr: "database not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := runCCConnectCommandHelper(t, "provider", tt.args...)
			if result.code == 0 {
				t.Fatalf("provider %v exited with code 0, want failure; stdout=%q stderr=%q", tt.args, result.stdout, result.stderr)
			}
			if tt.wantStderr != "" && !strings.Contains(result.stderr, tt.wantStderr) {
				t.Fatalf("stderr missing %q:\n%s", tt.wantStderr, result.stderr)
			}
			if tt.wantStdout != "" && !strings.Contains(result.stdout, tt.wantStdout) {
				t.Fatalf("stdout missing %q:\n%s", tt.wantStdout, result.stdout)
			}
		})
	}
}

func TestConfigFormatCommand_WriteFailureFromReadOnlyDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod read-only directory semantics differ on Windows")
	}

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(configPath, []byte("[log]\nlevel = \"info\"\n[[projects]]\nname = \"demo\"\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("chmod read-only dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(dir, 0o755)
	})

	result := runCCConnectCommandHelper(t, "config", "format", "--config", configPath)
	if result.code == 0 {
		t.Fatalf("config format exited with code 0, want failure; stdout=%q stderr=%q", result.stdout, result.stderr)
	}
	if !strings.Contains(result.stderr, "Error formatting config:") {
		t.Fatalf("stderr missing format error:\n%s", result.stderr)
	}
}

func writeProviderCommandConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	prev := config.ConfigPath
	t.Cleanup(func() {
		config.ConfigPath = prev
	})
	return path
}

func writeCCSwitchProviderDB(t *testing.T) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "cc-switch.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE providers (
id TEXT,
app_type TEXT,
name TEXT,
settings_config TEXT,
is_current INTEGER
)`); err != nil {
		t.Fatalf("create providers table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO providers (id, app_type, name, settings_config, is_current) VALUES (?, ?, ?, ?, ?)`,
		"p1",
		"claude",
		"Claude Relay",
		`{"env":{"ANTHROPIC_AUTH_TOKEN":"sk-claude","ANTHROPIC_BASE_URL":"https://claude.example","ANTHROPIC_MODEL":"opus"}}`,
		1,
	); err != nil {
		t.Fatalf("insert claude provider: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO providers (id, app_type, name, settings_config, is_current) VALUES (?, ?, ?, ?, ?)`,
		"p2",
		"codex",
		"Codex Relay",
		`{"auth":{"OPENAI_API_KEY":"sk-openai"},"config":"model = \"gpt-5\"\nbase_url = \"https://openai.example/v1\""}`,
		0,
	); err != nil {
		t.Fatalf("insert codex provider: %v", err)
	}
	return dbPath
}

func captureCommandOutput(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()
	oldStdout := os.Stdout
	oldStderr := os.Stderr
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}
	os.Stdout = outW
	os.Stderr = errW
	defer func() {
		os.Stdout = oldStdout
		os.Stderr = oldStderr
	}()

	fn()

	if err := outW.Close(); err != nil {
		t.Fatalf("close stdout writer: %v", err)
	}
	if err := errW.Close(); err != nil {
		t.Fatalf("close stderr writer: %v", err)
	}
	var outBuf, errBuf bytes.Buffer
	if _, err := outBuf.ReadFrom(outR); err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	if _, err := errBuf.ReadFrom(errR); err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	if err := outR.Close(); err != nil {
		t.Fatalf("close stdout reader: %v", err)
	}
	if err := errR.Close(); err != nil {
		t.Fatalf("close stderr reader: %v", err)
	}
	return outBuf.String(), errBuf.String()
}

func runCCConnectCommandHelper(t *testing.T, command string, args ...string) commandResult {
	t.Helper()
	cmdArgs := append([]string{"-test.run=TestCCConnectCommandHelperProcess", "--", command}, args...)
	cmd := exec.Command(os.Args[0], cmdArgs...)
	cmd.Env = append(os.Environ(), "CC_CONNECT_COMMAND_HELPER=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := commandResult{stdout: stdout.String(), stderr: stderr.String()}
	if exitErr, ok := err.(*exec.ExitError); ok {
		result.code = exitErr.ExitCode()
		return result
	}
	if err != nil {
		t.Fatalf("run helper command: %v", err)
	}
	return result
}

func TestCCConnectCommandHelperProcess(t *testing.T) {
	if os.Getenv("CC_CONNECT_COMMAND_HELPER") != "1" {
		return
	}
	idx := -1
	for i, arg := range os.Args {
		if arg == "--" {
			idx = i
			break
		}
	}
	if idx < 0 || idx+1 >= len(os.Args) {
		os.Exit(2)
	}
	command := os.Args[idx+1]
	args := os.Args[idx+2:]
	switch command {
	case "provider":
		runProviderCommand(args)
	case "config":
		runConfig(args)
	default:
		os.Exit(2)
	}
	os.Exit(0)
}
