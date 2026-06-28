package codex

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

const fakeCodexPowerShellPrelude = `
function fakeCodexArgs {
  if ([string]::IsNullOrWhiteSpace($env:CODEX_FAKE_ARGS_FILE) -or -not (Test-Path -LiteralPath $env:CODEX_FAKE_ARGS_FILE)) {
    return @()
  }
  return @(Get-Content -LiteralPath $env:CODEX_FAKE_ARGS_FILE)
}
`

func writeFakeCodexScript(t *testing.T, dir, shellScript, powershellScript string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		psPath := filepath.Join(dir, "codex.ps1")
		if err := os.WriteFile(psPath, []byte(fakeCodexPowerShellPrelude+powershellScript), 0o644); err != nil {
			t.Fatalf("write fake codex powershell script: %v", err)
		}
		cmdPath := filepath.Join(dir, "codex.cmd")
		cmdScript := "@echo off\r\n" +
			"setlocal\r\n" +
			"set \"CODEX_FAKE_SCRIPT=%~dp0codex.ps1\"\r\n" +
			"set \"CODEX_FAKE_ARGS_FILE=%TEMP%\\codex-fake-args-%RANDOM%-%RANDOM%.txt\"\r\n" +
			"type nul > \"%CODEX_FAKE_ARGS_FILE%\"\r\n" +
			":args\r\n" +
			"if \"%~1\"==\"\" goto run\r\n" +
			">> \"%CODEX_FAKE_ARGS_FILE%\" echo(%~1\r\n" +
			"shift\r\n" +
			"goto args\r\n" +
			":run\r\n" +
			"powershell -NoProfile -ExecutionPolicy Bypass -File \"%CODEX_FAKE_SCRIPT%\"\r\n" +
			"set \"CODEX_FAKE_EXIT=%ERRORLEVEL%\"\r\n" +
			"del \"%CODEX_FAKE_ARGS_FILE%\" >nul 2>nul\r\n" +
			"exit /b %CODEX_FAKE_EXIT%\r\n"
		if err := os.WriteFile(cmdPath, []byte(cmdScript), 0o755); err != nil {
			t.Fatalf("write fake codex cmd shim: %v", err)
		}
		return
	}
	scriptPath := filepath.Join(dir, "codex")
	if err := os.WriteFile(scriptPath, []byte(shellScript), 0o755); err != nil {
		t.Fatalf("write fake codex: %v", err)
	}
}

func waitForArgsFile(t *testing.T, path string, wantSequence []string) []string {
	t.Helper()
	var args []string
	waitUntil(t, 30*time.Second, "args file "+path, func() bool {
		data, err := os.ReadFile(path)
		if err != nil {
			return false
		}
		text := strings.TrimSpace(string(data))
		if text == "" {
			return false
		}
		lines := strings.Split(text, "\n")
		args = args[:0]
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" {
				args = append(args, line)
			}
		}
		return len(args) > 0 && containsSequence(args, wantSequence)
	}, func() string {
		data, err := os.ReadFile(path)
		if err != nil {
			return err.Error()
		}
		return "current=" + string(data) + ", want sequence=" + strings.Join(wantSequence, " ")
	})
	return append([]string(nil), args...)
}

type lockedWriteCloser struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (w *lockedWriteCloser) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *lockedWriteCloser) Close() error { return nil }

func (w *lockedWriteCloser) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

var _ io.WriteCloser = (*lockedWriteCloser)(nil)

func serverRequestProbe(t *testing.T, idJSON, method string, params any) map[string]json.RawMessage {
	t.Helper()
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	methodJSON, err := json.Marshal(method)
	if err != nil {
		t.Fatalf("marshal method: %v", err)
	}
	return map[string]json.RawMessage{
		"id":     json.RawMessage(idJSON),
		"method": methodJSON,
		"params": paramsJSON,
	}
}

func waitForWrittenJSONLine(t *testing.T, w *lockedWriteCloser) string {
	t.Helper()
	var line string
	waitUntil(t, time.Second, "JSON response", func() bool {
		for _, candidate := range strings.Split(w.String(), "\n") {
			candidate = strings.TrimSpace(candidate)
			if candidate != "" {
				line = candidate
				return true
			}
		}
		return false
	}, func() string {
		return "buffer=" + w.String()
	})
	return line
}
