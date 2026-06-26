package main

import (
	"net/http"
	"strings"
	"testing"
)

func TestTimerCommands_AddListInfoDeleteUseUnixAPI(t *testing.T) {
	dataDir, requests := startCommandAPIServer(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		switch r.URL.Path {
		case "/timer/add":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{
				"id":           "timer-add",
				"scheduled_at": "2026-06-26T10:30:00+08:00",
				"prompt":       body["prompt"],
				"exec":         body["exec"],
			})
		case "/timer/list":
			writeJSONResponse(t, w, http.StatusOK, []map[string]any{
				{"id": "timer-add", "scheduled_at": "2026-06-26T10:30:00+08:00", "prompt": "check PR", "mute": true},
				{"id": "timer-cmd", "scheduled_at": "2026-06-26T11:00:00+08:00", "exec": "df -h", "description": "Disk check"},
			})
		case "/timer/info":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{
				"id":           r.URL.Query().Get("id"),
				"scheduled_at": "2026-06-26T10:30:00+08:00",
				"project":      "timer-project",
				"prompt":       "check PR",
			})
		case "/timer/del":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"ok": true})
		default:
			http.NotFound(w, r)
		}
	})

	stdout, stderr := captureCronTimerOutput(t, func() {
		runTimerAdd([]string{
			"--data-dir", dataDir,
			"--project", "timer-project",
			"--session-key", "timer-session",
			"--delay", "30m",
			"--prompt", "check PR",
			"--desc", "PR follow-up",
			"--session-mode", "new-per-run",
			"--timeout-mins", "10",
			"--mute",
		})
	})
	if stderr != "" {
		t.Fatalf("runTimerAdd stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, "Timer created: timer-add") || !strings.Contains(stdout, "Fires at: 2026-06-26T10:30:00+08:00") {
		t.Fatalf("runTimerAdd stdout missing creation details:\n%s", stdout)
	}
	req := nextCommandAPIRequest(t, requests)
	if req.Method != http.MethodPost || req.Path != "/timer/add" {
		t.Fatalf("timer add request = %s %s, want POST /timer/add", req.Method, req.Path)
	}
	for key, want := range map[string]any{
		"project":      "timer-project",
		"session_key":  "timer-session",
		"delay":        "30m",
		"prompt":       "check PR",
		"description":  "PR follow-up",
		"session_mode": "new-per-run",
		"timeout_mins": float64(10),
		"mute":         true,
	} {
		if got := req.Body[key]; got != want {
			t.Fatalf("timer add body[%s] = %#v (%T), want %#v (%T); body=%v", key, got, got, want, want, req.Body)
		}
	}

	stdout, stderr = captureCronTimerOutput(t, func() {
		runTimerList([]string{"--data-dir", dataDir, "--project", "timer-project"})
	})
	if stderr != "" {
		t.Fatalf("runTimerList stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, "Pending timers (2)") || !strings.Contains(stdout, "timer-add") || !strings.Contains(stdout, "[mute]") || !strings.Contains(stdout, "Disk check") {
		t.Fatalf("runTimerList stdout missing timers:\n%s", stdout)
	}
	req = nextCommandAPIRequest(t, requests)
	if req.Method != http.MethodGet || req.Path != "/timer/list" || req.Query != "project=timer-project" {
		t.Fatalf("timer list request = %s %s?%s, want GET /timer/list?project=timer-project", req.Method, req.Path, req.Query)
	}

	stdout, stderr = captureCronTimerOutput(t, func() {
		runTimerInfo([]string{"--data-dir", dataDir, "timer-add"})
	})
	if stderr != "" {
		t.Fatalf("runTimerInfo stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, `"id": "timer-add"`) || !strings.Contains(stdout, `"project": "timer-project"`) {
		t.Fatalf("runTimerInfo stdout missing pretty JSON:\n%s", stdout)
	}
	req = nextCommandAPIRequest(t, requests)
	if req.Method != http.MethodGet || req.Path != "/timer/info" || req.Query != "id=timer-add" {
		t.Fatalf("timer info request = %s %s?%s, want GET /timer/info?id=timer-add", req.Method, req.Path, req.Query)
	}

	stdout, stderr = captureCronTimerOutput(t, func() {
		runTimerDel([]string{"--data-dir", dataDir, "timer-add"})
	})
	if stderr != "" {
		t.Fatalf("runTimerDel stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, "Timer timer-add cancelled.") {
		t.Fatalf("runTimerDel stdout missing cancel message:\n%s", stdout)
	}
	req = nextCommandAPIRequest(t, requests)
	if req.Method != http.MethodPost || req.Path != "/timer/del" || req.Body["id"] != "timer-add" {
		t.Fatalf("timer del request = %+v, want id timer-add", req)
	}
}

func TestTimerAdd_PositionalDelayAndEnvContext(t *testing.T) {
	t.Setenv("CC_PROJECT", "env-project")
	t.Setenv("CC_SESSION_KEY", "env-session")
	dataDir, requests := startCommandAPIServer(t, func(w http.ResponseWriter, _ *http.Request, body map[string]any) {
		writeJSONResponse(t, w, http.StatusOK, map[string]any{
			"id":           "timer-pos",
			"scheduled_at": "2026-06-26T12:00:00+08:00",
			"prompt":       body["prompt"],
		})
	})

	stdout, stderr := captureCronTimerOutput(t, func() {
		runTimerAdd([]string{"--data-dir", dataDir, "2h", "Check", "PR", "status"})
	})
	if stderr != "" {
		t.Fatalf("runTimerAdd stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, "Timer created: timer-pos") {
		t.Fatalf("runTimerAdd stdout missing success:\n%s", stdout)
	}
	req := nextCommandAPIRequest(t, requests)
	for key, want := range map[string]any{
		"project":     "env-project",
		"session_key": "env-session",
		"delay":       "2h",
		"prompt":      "Check PR status",
		"mute":        false,
	} {
		if got := req.Body[key]; got != want {
			t.Fatalf("timer add body[%s] = %#v, want %#v; body=%v", key, got, want, req.Body)
		}
	}
}

func TestTimerAdd_AtTimeAndExecRequest(t *testing.T) {
	dataDir, requests := startCommandAPIServer(t, func(w http.ResponseWriter, _ *http.Request, body map[string]any) {
		writeJSONResponse(t, w, http.StatusOK, map[string]any{
			"id":           "timer-at",
			"scheduled_at": "2026-06-26T09:00:00+08:00",
			"exec":         body["exec"],
		})
	})

	stdout, stderr := captureCronTimerOutput(t, func() {
		runTimerAdd([]string{
			"--data-dir", dataDir,
			"--at", "2026-06-26T09:00",
			"--exec", "df -h",
			"--desc", "Disk check",
		})
	})
	if stderr != "" {
		t.Fatalf("runTimerAdd stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, "Command: df -h") {
		t.Fatalf("runTimerAdd stdout missing exec command:\n%s", stdout)
	}
	req := nextCommandAPIRequest(t, requests)
	if req.Body["delay"] != "2026-06-26T09:00" || req.Body["exec"] != "df -h" || req.Body["prompt"] != "" {
		t.Fatalf("timer add at-time body = %v, want at time exec request", req.Body)
	}
}

func TestTimerCommands_ServerValidationErrorsExit(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		status     int
		message    string
		wantStderr string
	}{
		{
			name:       "invalid duration",
			args:       []string{"add", "--delay", "later", "--prompt", "bad duration"},
			status:     http.StatusBadRequest,
			message:    "invalid duration",
			wantStderr: "invalid duration",
		},
		{
			name:       "cancel missing timer",
			args:       []string{"del", "timer-missing"},
			status:     http.StatusNotFound,
			message:    "timer not found",
			wantStderr: "timer not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dataDir, _ := startCommandAPIServer(t, func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
				http.Error(w, tt.message, tt.status)
			})
			args := append([]string(nil), tt.args...)
			if len(args) > 0 {
				args = append(args[:1], append([]string{"--data-dir", dataDir}, args[1:]...)...)
			}
			result := runCronTimerCommandHelper(t, "timer", args...)
			if result.code == 0 {
				t.Fatalf("timer %v exited with code 0, want failure; stdout=%q stderr=%q", args, result.stdout, result.stderr)
			}
			if !strings.Contains(result.stderr, tt.wantStderr) {
				t.Fatalf("stderr missing %q:\n%s", tt.wantStderr, result.stderr)
			}
		})
	}
}
