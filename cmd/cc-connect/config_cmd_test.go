package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	ccconnect "github.com/chenhg5/cc-connect"
)

func TestConfigCommands_ExamplePathAndUsage(t *testing.T) {
	stdout, stderr := captureCommandOutput(t, func() {
		runConfig([]string{"example"})
	})
	if stderr != "" {
		t.Fatalf("runConfig(example) stderr = %q, want empty", stderr)
	}
	if stdout != ccconnect.ConfigExampleTOML {
		t.Fatalf("runConfig(example) stdout length = %d, want example length %d", len(stdout), len(ccconnect.ConfigExampleTOML))
	}

	stdout, stderr = captureCommandOutput(t, func() {
		runConfig([]string{"path"})
	})
	if stderr != "" {
		t.Fatalf("runConfig(path) stderr = %q, want empty", stderr)
	}
	if !strings.HasSuffix(strings.TrimSpace(stdout), filepath.Join(".cc-connect", "config.toml")) {
		t.Fatalf("runConfig(path) stdout = %q, want default config path", stdout)
	}
}

func TestConfigFormatCommand_FormatsConfigFile(t *testing.T) {
	configPath := writeProviderCommandConfig(t, "[log]\nlevel = \"info\"\n\n\n[[projects]]\nname = \"demo\"\n\n[projects.agent]\ntype = \"claudecode\"\n\n")

	stdout, stderr := captureCommandOutput(t, func() {
		runConfig([]string{"format", "--config", configPath})
	})
	if stderr != "" {
		t.Fatalf("runConfig(format) stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, "Formatted "+configPath) {
		t.Fatalf("runConfig(format) stdout = %q, want formatted message", stdout)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read formatted config: %v", err)
	}
	formatted := string(data)
	if strings.Contains(formatted, "\n\n\n") {
		t.Fatalf("formatted config still contains repeated blank lines:\n%s", formatted)
	}

	stdout, stderr = captureCommandOutput(t, func() {
		runConfig([]string{"fmt", "--config", configPath})
	})
	if stderr != "" {
		t.Fatalf("runConfig(fmt) stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, "Formatted "+configPath) {
		t.Fatalf("runConfig(fmt) stdout = %q, want formatted message", stdout)
	}
}

func TestConfigCommands_ExitFailures(t *testing.T) {
	missingPath := filepath.Join(t.TempDir(), "missing.toml")
	invalidPath := filepath.Join(t.TempDir(), "invalid.toml")
	if err := os.WriteFile(invalidPath, []byte("[broken\n"), 0o644); err != nil {
		t.Fatalf("write invalid config: %v", err)
	}

	tests := []struct {
		name       string
		args       []string
		wantStderr string
	}{
		{
			name:       "missing subcommand",
			args:       nil,
			wantStderr: "Usage: cc-connect config <subcommand>",
		},
		{
			name:       "unknown subcommand",
			args:       []string{"get", "language"},
			wantStderr: "Unknown config subcommand: get",
		},
		{
			name:       "set is not a CLI subcommand",
			args:       []string{"set", "language", "zh"},
			wantStderr: "Unknown config subcommand: set",
		},
		{
			name:       "list is not a CLI subcommand",
			args:       []string{"list"},
			wantStderr: "Unknown config subcommand: list",
		},
		{
			name:       "format missing file",
			args:       []string{"format", "--config", missingPath},
			wantStderr: "Config file not found:",
		},
		{
			name:       "format invalid toml",
			args:       []string{"format", "--config", invalidPath},
			wantStderr: "Error formatting config:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := runCCConnectCommandHelper(t, "config", tt.args...)
			if result.code == 0 {
				t.Fatalf("config %v exited with code 0, want failure; stdout=%q stderr=%q", tt.args, result.stdout, result.stderr)
			}
			if !strings.Contains(result.stderr, tt.wantStderr) {
				t.Fatalf("stderr missing %q:\n%s", tt.wantStderr, result.stderr)
			}
		})
	}
}
