package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type commandAPIRequest struct {
	Method string
	Path   string
	Query  string
	Body   map[string]any
}

type cronTimerCommandResult struct {
	stdout string
	stderr string
	code   int
}

func startCommandAPIServer(t *testing.T, handler func(http.ResponseWriter, *http.Request, map[string]any)) (string, <-chan commandAPIRequest) {
	t.Helper()

	dataDir, err := os.MkdirTemp("/tmp", "cc-connect-cmd-test-")
	if err != nil {
		t.Fatalf("create short temp dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(dataDir)
	})
	runDir := filepath.Join(dataDir, "run")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatalf("mkdir run dir: %v", err)
	}
	sockPath := filepath.Join(runDir, "api.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen unix socket: %v", err)
	}

	requests := make(chan commandAPIRequest, 16)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var body map[string]any
		if r.Body != nil {
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err.Error() != "EOF" {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
		}
		requests <- commandAPIRequest{
			Method: r.Method,
			Path:   r.URL.Path,
			Query:  r.URL.RawQuery,
			Body:   body,
		}
		handler(w, r, body)
	})}
	go func() {
		_ = server.Serve(ln)
	}()
	t.Cleanup(func() {
		_ = server.Shutdown(context.Background())
		_ = os.Remove(sockPath)
	})

	return dataDir, requests
}

func nextCommandAPIRequest(t *testing.T, requests <-chan commandAPIRequest) commandAPIRequest {
	t.Helper()
	select {
	case req := <-requests:
		return req
	default:
		t.Fatal("no command API request recorded")
		return commandAPIRequest{}
	}
}

func writeJSONResponse(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("write response: %v", err)
	}
}

func captureCronTimerOutput(t *testing.T, fn func()) (stdout, stderr string) {
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

func TestCronCommands_AddListEditExecDeleteUseUnixAPI(t *testing.T) {
	dataDir, requests := startCommandAPIServer(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		switch r.URL.Path {
		case "/cron/add":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{
				"id":        "job-add",
				"cron_expr": body["cron_expr"],
				"prompt":    body["prompt"],
				"exec":      body["exec"],
			})
		case "/cron/list":
			writeJSONResponse(t, w, http.StatusOK, []map[string]any{
				{"id": "job-add", "cron_expr": "0 9 * * *", "prompt": "standup", "enabled": true},
				{"id": "job-cmd", "cron_expr": "*/30 * * * *", "exec": "df -h", "description": "Disk check", "enabled": false},
			})
		case "/cron/edit":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{
				"id":    body["id"],
				"field": body["field"],
				"value": body["value"],
			})
		case "/cron/exec":
			writeJSONResponse(t, w, http.StatusAccepted, map[string]any{"ok": true})
		case "/cron/del":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"ok": true})
		default:
			http.NotFound(w, r)
		}
	})

	stdout, stderr := captureCronTimerOutput(t, func() {
		runCronAdd([]string{
			"--data-dir", dataDir,
			"--project", "agent-project",
			"--session-key", "session-1",
			"--cron", "0 9 * * *",
			"--prompt", "standup",
			"--desc", "Daily standup",
			"--session-mode", "new-per-run",
			"--timeout-mins", "45",
			"--silent",
		})
	})
	if stderr != "" {
		t.Fatalf("runCronAdd stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, "Cron job created: job-add") || !strings.Contains(stdout, "Schedule: 0 9 * * *") {
		t.Fatalf("runCronAdd stdout missing creation details:\n%s", stdout)
	}
	req := nextCommandAPIRequest(t, requests)
	if req.Method != http.MethodPost || req.Path != "/cron/add" {
		t.Fatalf("cron add request = %s %s, want POST /cron/add", req.Method, req.Path)
	}
	for key, want := range map[string]any{
		"project":      "agent-project",
		"session_key":  "session-1",
		"cron_expr":    "0 9 * * *",
		"prompt":       "standup",
		"description":  "Daily standup",
		"session_mode": "new-per-run",
		"timeout_mins": float64(45),
		"silent":       true,
	} {
		if got := req.Body[key]; got != want {
			t.Fatalf("cron add body[%s] = %#v (%T), want %#v (%T); body=%v", key, got, got, want, want, req.Body)
		}
	}

	stdout, stderr = captureCronTimerOutput(t, func() {
		runCronList([]string{"--data-dir", dataDir, "--project", "agent-project"})
	})
	if stderr != "" {
		t.Fatalf("runCronList stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, "Scheduled tasks (2)") || !strings.Contains(stdout, "job-add") || !strings.Contains(stdout, "Disk check") {
		t.Fatalf("runCronList stdout missing jobs:\n%s", stdout)
	}
	req = nextCommandAPIRequest(t, requests)
	if req.Method != http.MethodGet || req.Path != "/cron/list" || req.Query != "project=agent-project" {
		t.Fatalf("cron list request = %s %s?%s, want GET /cron/list?project=agent-project", req.Method, req.Path, req.Query)
	}

	stdout, stderr = captureCronTimerOutput(t, func() {
		runCronEdit([]string{"--data-dir", dataDir, "job-add", "work_dir", "/tmp/workspace"})
	})
	if stderr != "" {
		t.Fatalf("runCronEdit stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, "Updated job job-add") || !strings.Contains(stdout, "/tmp/workspace") {
		t.Fatalf("runCronEdit stdout missing updated field:\n%s", stdout)
	}
	req = nextCommandAPIRequest(t, requests)
	if req.Method != http.MethodPost || req.Path != "/cron/edit" || req.Body["id"] != "job-add" || req.Body["field"] != "work_dir" || req.Body["value"] != "/tmp/workspace" {
		t.Fatalf("cron edit request = %+v, want work_dir update", req)
	}

	stdout, stderr = captureCronTimerOutput(t, func() {
		runCronExec([]string{"--data-dir", dataDir, "job-add"})
	})
	if stderr != "" {
		t.Fatalf("runCronExec stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, "Triggered cron job: job-add") {
		t.Fatalf("runCronExec stdout missing trigger message:\n%s", stdout)
	}
	req = nextCommandAPIRequest(t, requests)
	if req.Method != http.MethodPost || req.Path != "/cron/exec" || req.Body["id"] != "job-add" {
		t.Fatalf("cron exec request = %+v, want id job-add", req)
	}

	stdout, stderr = captureCronTimerOutput(t, func() {
		runCronDel([]string{"--data-dir", dataDir, "job-add"})
	})
	if stderr != "" {
		t.Fatalf("runCronDel stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, "Cron job job-add deleted.") {
		t.Fatalf("runCronDel stdout missing delete message:\n%s", stdout)
	}
	req = nextCommandAPIRequest(t, requests)
	if req.Method != http.MethodPost || req.Path != "/cron/del" || req.Body["id"] != "job-add" {
		t.Fatalf("cron del request = %+v, want id job-add", req)
	}
}

func TestCronAdd_PositionalExpressionAndEnvContext(t *testing.T) {
	t.Setenv("CC_PROJECT", "env-project")
	t.Setenv("CC_SESSION_KEY", "env-session")
	dataDir, requests := startCommandAPIServer(t, func(w http.ResponseWriter, _ *http.Request, body map[string]any) {
		writeJSONResponse(t, w, http.StatusOK, map[string]any{
			"id":        "job-pos",
			"cron_expr": body["cron_expr"],
			"prompt":    body["prompt"],
		})
	})

	stdout, stderr := captureCronTimerOutput(t, func() {
		runCronAdd([]string{"--data-dir", dataDir, "0", "6", "*", "*", "*", "Collect", "status"})
	})
	if stderr != "" {
		t.Fatalf("runCronAdd stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, "Cron job created: job-pos") {
		t.Fatalf("runCronAdd stdout missing success:\n%s", stdout)
	}
	req := nextCommandAPIRequest(t, requests)
	for key, want := range map[string]any{
		"project":     "env-project",
		"session_key": "env-session",
		"cron_expr":   "0 6 * * *",
		"prompt":      "Collect status",
	} {
		if got := req.Body[key]; got != want {
			t.Fatalf("cron add body[%s] = %#v, want %#v; body=%v", key, got, want, req.Body)
		}
	}
}

func TestCronCommands_ServerValidationErrorsExit(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		status     int
		message    string
		wantStderr string
	}{
		{
			name:       "invalid cron expression",
			args:       []string{"add", "--cron", "not-a-cron", "--prompt", "bad expr"},
			status:     http.StatusBadRequest,
			message:    "invalid cron expression",
			wantStderr: "invalid cron expression",
		},
		{
			name:       "duplicate job",
			args:       []string{"add", "--cron", "0 9 * * *", "--prompt", "standup"},
			status:     http.StatusConflict,
			message:    "duplicate cron job",
			wantStderr: "duplicate cron job",
		},
		{
			name:       "edit bad bool",
			args:       []string{"edit", "job-1", "enabled", "maybe"},
			status:     http.StatusOK,
			message:    "",
			wantStderr: "enabled must be true or false",
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
			result := runCronTimerCommandHelper(t, "cron", args...)
			if result.code == 0 {
				t.Fatalf("cron %v exited with code 0, want failure; stdout=%q stderr=%q", args, result.stdout, result.stderr)
			}
			if !strings.Contains(result.stderr, tt.wantStderr) {
				t.Fatalf("stderr missing %q:\n%s", tt.wantStderr, result.stderr)
			}
		})
	}
}

func runCronTimerCommandHelper(t *testing.T, command string, args ...string) cronTimerCommandResult {
	t.Helper()
	cmdArgs := append([]string{"-test.run=TestCronTimerCommandHelperProcess", "--", command}, args...)
	cmd := exec.Command(os.Args[0], cmdArgs...)
	cmd.Env = append(os.Environ(), "CC_CONNECT_CRON_TIMER_HELPER=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := cronTimerCommandResult{stdout: stdout.String(), stderr: stderr.String()}
	if exitErr, ok := err.(*exec.ExitError); ok {
		result.code = exitErr.ExitCode()
		return result
	}
	if err != nil {
		t.Fatalf("run helper command: %v", err)
	}
	return result
}

func TestCronTimerCommandHelperProcess(t *testing.T) {
	if os.Getenv("CC_CONNECT_CRON_TIMER_HELPER") != "1" {
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
	case "cron":
		runCron(args)
	case "timer":
		runTimer(args)
	default:
		os.Exit(2)
	}
	os.Exit(0)
}
