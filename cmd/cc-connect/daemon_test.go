package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestParseDaemonInstallArgs_ConfigSetsWorkDir(t *testing.T) {
	cfg, force, err := parseDaemonInstallArgs([]string{"--config", "/tmp/example/config.toml"})
	if err != nil {
		t.Fatalf("parseDaemonInstallArgs returned error: %v", err)
	}
	if force {
		t.Fatalf("force = true, want false")
	}

	want := filepath.Clean("/tmp/example")
	if cfg.WorkDir != want {
		t.Fatalf("cfg.WorkDir = %q, want %q", cfg.WorkDir, want)
	}
}

func TestParseDaemonInstallArgs_ConfigEqualsFormSetsWorkDir(t *testing.T) {
	cfg, _, err := parseDaemonInstallArgs([]string{"--config=/tmp/example/config.toml"})
	if err != nil {
		t.Fatalf("parseDaemonInstallArgs returned error: %v", err)
	}

	want := filepath.Clean("/tmp/example")
	if cfg.WorkDir != want {
		t.Fatalf("cfg.WorkDir = %q, want %q", cfg.WorkDir, want)
	}
}

func TestParseDaemonInstallArgs_NoCaptureSecretsFlag(t *testing.T) {
	os.Unsetenv("CC_DAEMON_NO_CAPTURE_SECRETS")

	cfg, _, err := parseDaemonInstallArgs([]string{"--no-capture-secrets"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !cfg.NoCaptureSecrets {
		t.Fatal("flag should set NoCaptureSecrets=true")
	}

	cfg2, _, err := parseDaemonInstallArgs(nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg2.NoCaptureSecrets {
		t.Fatal("default must be false when flag and env are unset")
	}
}

func TestParseDaemonInstallArgs_NoCaptureSecretsEnv(t *testing.T) {
	for _, v := range []string{"1", "true", "TRUE", "yes", "on"} {
		t.Run("truthy="+v, func(t *testing.T) {
			t.Setenv("CC_DAEMON_NO_CAPTURE_SECRETS", v)
			cfg, _, err := parseDaemonInstallArgs(nil)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if !cfg.NoCaptureSecrets {
				t.Fatalf("env=%q should opt out", v)
			}
		})
	}
	for _, v := range []string{"0", "false", "", "no", "off"} {
		t.Run("falsy="+v, func(t *testing.T) {
			t.Setenv("CC_DAEMON_NO_CAPTURE_SECRETS", v)
			cfg, _, err := parseDaemonInstallArgs(nil)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if cfg.NoCaptureSecrets {
				t.Fatalf("env=%q should NOT opt out", v)
			}
		})
	}
}

func TestParseDaemonInstallArgs_NoCaptureSecretsFlagAndEnvCombine(t *testing.T) {
	// OR semantics: env=truthy + flag=present → still true.
	t.Setenv("CC_DAEMON_NO_CAPTURE_SECRETS", "1")
	cfg, _, err := parseDaemonInstallArgs([]string{"--no-capture-secrets", "--force"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !cfg.NoCaptureSecrets {
		t.Fatal("flag+env both should leave NoCaptureSecrets=true")
	}
	// env=truthy without flag → still true.
	cfg2, _, err := parseDaemonInstallArgs([]string{"--force"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !cfg2.NoCaptureSecrets {
		t.Fatal("env=1 alone should opt out")
	}
}

func TestParseDaemonInstallArgs_WorkDirOverridesConfig(t *testing.T) {
	cfg, force, err := parseDaemonInstallArgs([]string{
		"--config", "/tmp/example/config.toml",
		"--work-dir", "/tmp/override",
		"--force",
	})
	if err != nil {
		t.Fatalf("parseDaemonInstallArgs returned error: %v", err)
	}
	if !force {
		t.Fatalf("force = false, want true")
	}

	want := filepath.Clean("/tmp/override")
	if cfg.WorkDir != want {
		t.Fatalf("cfg.WorkDir = %q, want %q", cfg.WorkDir, want)
	}
}

func TestParseDaemonInstallArgs_LogFlagsAndErrors(t *testing.T) {
	cfg, force, err := parseDaemonInstallArgs([]string{
		"--log-file", "/tmp/cc.log",
		"--log-max-size=32",
		"--force",
	})
	if err != nil {
		t.Fatalf("parseDaemonInstallArgs returned error: %v", err)
	}
	if !force {
		t.Fatalf("force = false, want true")
	}
	if cfg.LogFile != "/tmp/cc.log" {
		t.Fatalf("LogFile = %q, want /tmp/cc.log", cfg.LogFile)
	}
	if cfg.LogMaxSize != 32*1024*1024 {
		t.Fatalf("LogMaxSize = %d, want 32MiB", cfg.LogMaxSize)
	}

	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "missing log file", args: []string{"--log-file"}, want: "missing value for --log-file"},
		{name: "bad log max", args: []string{"--log-max-size", "abc"}, want: "invalid value for --log-max-size"},
		{name: "unknown", args: []string{"--bogus"}, want: "unknown flag: --bogus"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := parseDaemonInstallArgs(tt.args)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("parseDaemonInstallArgs(%v) error = %v, want containing %q", tt.args, err, tt.want)
			}
		})
	}
}

func TestDaemonLogsPrintsRequestedTail(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "cc-connect.log")
	if err := os.WriteFile(logFile, []byte("one\ntwo\nthree\nfour\n"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	stdout, stderr := captureCronTimerOutput(t, func() {
		daemonLogs([]string{"--log-file", logFile, "-n", "2"})
	})
	if stderr != "" {
		t.Fatalf("daemonLogs stderr = %q, want empty", stderr)
	}
	if stdout != "three\nfour\n" {
		t.Fatalf("daemonLogs stdout = %q, want last two lines", stdout)
	}
}

func TestDaemonCommandStatusStartStopBranchesWithFakeLaunchd(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("fake launchd command coverage is darwin-specific")
	}
	home := t.TempDir()
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "launchctl.log")
	writeFakeLaunchctl(t, binDir, logPath)

	plistDir := filepath.Join(home, "Library", "LaunchAgents")
	if err := os.MkdirAll(plistDir, 0o755); err != nil {
		t.Fatalf("mkdir plist dir: %v", err)
	}
	plistPath := filepath.Join(plistDir, "com.cc-connect.service.plist")
	if err := os.WriteFile(plistPath, []byte("<plist></plist>"), 0o644); err != nil {
		t.Fatalf("write plist: %v", err)
	}

	env := []string{
		"HOME=" + home,
		"PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"CC_CONNECT_FAKE_LAUNCHCTL_LOG=" + logPath,
	}

	status := runDaemonCommandHelper(t, env, "status")
	if status.code != 0 || !strings.Contains(status.stdout, "Status:    Running") || !strings.Contains(status.stdout, "PID:       4321") {
		t.Fatalf("daemon status result = %+v, want running status", status)
	}

	start := runDaemonCommandHelper(t, env, "start")
	if start.code != 0 || !strings.Contains(start.stdout, "cc-connect daemon started.") {
		t.Fatalf("daemon start result = %+v, want success", start)
	}

	stop := runDaemonCommandHelper(t, env, "stop")
	if stop.code != 0 || !strings.Contains(stop.stdout, "cc-connect daemon stopped.") {
		t.Fatalf("daemon stop result = %+v, want success", stop)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read fake launchctl log: %v", err)
	}
	logText := string(logData)
	if !strings.Contains(logText, "kickstart -kp") || !strings.Contains(logText, "bootout ") {
		t.Fatalf("fake launchctl log missing start/stop calls:\n%s", logText)
	}
}

func TestDaemonCommandNotInstalledAndUnknownBranches(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("daemon manager command behavior is platform-specific")
	}
	home := t.TempDir()
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "launchctl.log")
	writeFakeLaunchctl(t, binDir, logPath)
	env := []string{
		"HOME=" + home,
		"PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"CC_CONNECT_FAKE_LAUNCHCTL_LOG=" + logPath,
	}

	status := runDaemonCommandHelper(t, env, "status")
	if status.code != 0 || !strings.Contains(status.stdout, "Status:    Not installed") {
		t.Fatalf("daemon status result = %+v, want not installed", status)
	}

	start := runDaemonCommandHelper(t, env, "start")
	if start.code == 0 || !strings.Contains(start.stderr, "Service is not installed") {
		t.Fatalf("daemon start result = %+v, want not installed failure", start)
	}

	unknown := runDaemonCommandHelper(t, env, "bogus")
	if unknown.code == 0 || !strings.Contains(unknown.stderr, "Unknown daemon command: bogus") || !strings.Contains(unknown.stdout, "Usage: cc-connect daemon") {
		t.Fatalf("daemon unknown result = %+v, want usage failure", unknown)
	}
}

type p9DaemonCommandResult struct {
	stdout string
	stderr string
	code   int
}

func runDaemonCommandHelper(t *testing.T, extraEnv []string, args ...string) p9DaemonCommandResult {
	t.Helper()
	cmdArgs := append([]string{"-test.run=TestP9DaemonCommandHelperProcess", "--"}, args...)
	cmd := exec.Command(os.Args[0], cmdArgs...)
	cmd.Env = append(os.Environ(), "CC_CONNECT_P9_DAEMON_HELPER=1")
	cmd.Env = append(cmd.Env, extraEnv...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := p9DaemonCommandResult{stdout: stdout.String(), stderr: stderr.String()}
	if exitErr, ok := err.(*exec.ExitError); ok {
		result.code = exitErr.ExitCode()
		return result
	}
	if err != nil {
		t.Fatalf("run daemon helper command: %v", err)
	}
	return result
}

func TestP9DaemonCommandHelperProcess(t *testing.T) {
	if os.Getenv("CC_CONNECT_P9_DAEMON_HELPER") != "1" {
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
	runDaemon(os.Args[idx+1:])
	os.Exit(0)
}

func writeFakeLaunchctl(t *testing.T, binDir, logPath string) {
	t.Helper()
	script := `#!/bin/sh
echo "$@" >> "$CC_CONNECT_FAKE_LAUNCHCTL_LOG"
case "$1" in
  print)
    case "$2" in
      gui/*/com.cc-connect.service|user/*/com.cc-connect.service)
        printf 'pid = 4321\nstate = running\n'
        exit 0
        ;;
      gui/*|user/*)
        exit 0
        ;;
    esac
    ;;
  kickstart|bootout|bootstrap)
    exit 0
    ;;
esac
exit 1
`
	if err := os.WriteFile(filepath.Join(binDir, "launchctl"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake launchctl: %v", err)
	}
}
