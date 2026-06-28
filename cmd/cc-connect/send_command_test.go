package main

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chenhg5/cc-connect/core"
)

func TestParseSendArgs_MessageContextAttachmentsAndStdin(t *testing.T) {
	t.Setenv("CC_PROJECT", "env-project")
	t.Setenv("CC_SESSION_KEY", "env-session")
	t.Setenv("CC_CONNECT_MAX_ATTACHMENT_SIZE_MB", "1")

	imagePath := filepath.Join(t.TempDir(), "tiny.png")
	png, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/p9sAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatalf("decode png: %v", err)
	}
	if err := os.WriteFile(imagePath, png, 0o644); err != nil {
		t.Fatalf("write image: %v", err)
	}
	filePath := filepath.Join(t.TempDir(), "notes.md")
	if err := os.WriteFile(filePath, []byte("# notes\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	audioPath := filepath.Join(t.TempDir(), "voice.mp3")
	if err := os.WriteFile(audioPath, []byte("not real mp3 but extension is allowed"), 0o644); err != nil {
		t.Fatalf("write audio: %v", err)
	}
	videoPath := filepath.Join(t.TempDir(), "clip.mp4")
	if err := os.WriteFile(videoPath, []byte("not real mp4 but extension is allowed"), 0o644); err != nil {
		t.Fatalf("write video: %v", err)
	}

	withStdin(t, " message from stdin \n", func() {
		req, dataDir, err := parseSendArgs([]string{
			"--stdin",
			"--cwd", "/tmp/work",
			"--tts", "voice text",
			"--image", imagePath,
			"--file", filePath,
			"--audio", audioPath,
			"--video", videoPath,
			"--at-users", "ou_1, ou_2,,",
			"--at-all",
			"--data-dir", "/tmp/data",
			"ignored", "positional",
		})
		if err != nil {
			t.Fatalf("parseSendArgs returned error: %v", err)
		}
		if dataDir != "/tmp/data" {
			t.Fatalf("dataDir = %q, want /tmp/data", dataDir)
		}
		if req.Project != "env-project" || req.SessionKey != "env-session" {
			t.Fatalf("env context not applied: project=%q session=%q", req.Project, req.SessionKey)
		}
		if req.Message != "message from stdin" || req.WorkDir != "/tmp/work" || req.TTSText != "voice text" {
			t.Fatalf("message/workdir/tts mismatch: %+v", req)
		}
		if len(req.Images) != 1 || req.Images[0].FileName != "tiny.png" || !strings.HasPrefix(req.Images[0].MimeType, "image/") {
			t.Fatalf("image attachment mismatch: %+v", req.Images)
		}
		if len(req.Files) != 1 || req.Files[0].FileName != "notes.md" || !strings.HasPrefix(req.Files[0].MimeType, "text/markdown") {
			t.Fatalf("file attachment mismatch: %+v", req.Files)
		}
		if len(req.Audios) != 1 || req.Audios[0].FileName != "voice.mp3" {
			t.Fatalf("audio attachment mismatch: %+v", req.Audios)
		}
		if len(req.Videos) != 1 || req.Videos[0].FileName != "clip.mp4" {
			t.Fatalf("video attachment mismatch: %+v", req.Videos)
		}
		if !req.AtAll || len(req.AtUsers) != 2 || req.AtUsers[0] != "ou_1" || req.AtUsers[1] != "ou_2" {
			t.Fatalf("at fields mismatch: atAll=%v atUsers=%v", req.AtAll, req.AtUsers)
		}
	})
}

func TestParseSendArgs_PositionalMessageExplicitTargetAndErrors(t *testing.T) {
	req, dataDir, err := parseSendArgs([]string{
		"--project", "project-a",
		"--session", "feishu:chat:user",
		"--work-dir", "/workspace",
		"hello", "there",
	})
	if err != nil {
		t.Fatalf("parseSendArgs returned error: %v", err)
	}
	if dataDir != "" {
		t.Fatalf("dataDir = %q, want empty", dataDir)
	}
	if req.Project != "project-a" || req.SessionKey != "feishu:chat:user" || req.Message != "hello there" || req.WorkDir != "/workspace" {
		t.Fatalf("parsed request mismatch: %+v", req)
	}

	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "missing message", args: nil, want: "message, tts text, or attachment is required"},
		{name: "project value", args: []string{"--project"}, want: "--project requires a value"},
		{name: "session value", args: []string{"--session"}, want: "--session requires a value"},
		{name: "image path", args: []string{"--image"}, want: "--image requires a path"},
		{name: "missing attachment", args: []string{"--file", filepath.Join(t.TempDir(), "missing.txt")}, want: "read attachment"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := parseSendArgs(tt.args)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("parseSendArgs(%v) error = %v, want containing %q", tt.args, err, tt.want)
			}
		})
	}
}

func TestRunSend_PostsPayloadToUnixAPI(t *testing.T) {
	dataDir, requests := startCommandAPIServer(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		if r.URL.Path != "/send" {
			http.NotFound(w, r)
			return
		}
		if body["message"] != "hello api" {
			http.Error(w, "wrong message", http.StatusBadRequest)
			return
		}
		writeJSONResponse(t, w, http.StatusOK, map[string]any{"ok": true})
	})

	stdout, stderr := captureP8CommandOutput(t, func() {
		runSend([]string{
			"--data-dir", dataDir,
			"--project", "send-project",
			"--session", "send-session",
			"--cwd", "/tmp/send-work",
			"--message", "hello api",
			"--at-users", "ou_a,ou_b",
			"--at-all",
		})
	})
	if stderr != "" {
		t.Fatalf("runSend stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, "Message sent successfully.") {
		t.Fatalf("runSend stdout missing success: %q", stdout)
	}
	req := nextCommandAPIRequest(t, requests)
	if req.Method != http.MethodPost || req.Path != "/send" {
		t.Fatalf("send request = %s %s, want POST /send", req.Method, req.Path)
	}
	for key, want := range map[string]any{
		"project":     "send-project",
		"session_key": "send-session",
		"message":     "hello api",
		"work_dir":    "/tmp/send-work",
		"at_all":      true,
	} {
		if got := req.Body[key]; got != want {
			t.Fatalf("send body[%s] = %#v, want %#v; body=%v", key, got, want, req.Body)
		}
	}
	atUsers, ok := req.Body["at_users"].([]any)
	if !ok || len(atUsers) != 2 || atUsers[0] != "ou_a" || atUsers[1] != "ou_b" {
		t.Fatalf("send body at_users = %#v, want two users", req.Body["at_users"])
	}
}

func TestRunSend_ErrorReturnsExit(t *testing.T) {
	dataDir, _ := startCommandAPIServer(t, func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
		http.Error(w, "session not found", http.StatusNotFound)
	})

	result := runCommandHelper(t, "send", "--data-dir", dataDir, "-m", "hello")
	if result.code == 0 || !strings.Contains(result.stderr, "session not found") {
		t.Fatalf("send server failure result = %+v, want session not found", result)
	}

	missingSocketDir := t.TempDir()
	result = runCommandHelper(t, "send", "--data-dir", missingSocketDir, "-m", "hello")
	if result.code == 0 || !strings.Contains(result.stderr, "socket not found") {
		t.Fatalf("send missing socket result = %+v, want socket not found", result)
	}
}

func TestSendPayloadRoundTrip(t *testing.T) {
	want := core.SendRequest{
		Project:    "project",
		SessionKey: "session",
		Message:    "hello",
		WorkDir:    "/tmp/work",
		AtUsers:    []string{"ou_1"},
		AtAll:      true,
	}
	payload, err := buildSendPayload(want)
	if err != nil {
		t.Fatalf("buildSendPayload: %v", err)
	}
	var got core.SendRequest
	if err := decodeSendPayload(payload, &got); err != nil {
		t.Fatalf("decodeSendPayload: %v", err)
	}
	if got.Project != want.Project || got.SessionKey != want.SessionKey || got.Message != want.Message || got.WorkDir != want.WorkDir || !got.AtAll || len(got.AtUsers) != 1 || got.AtUsers[0] != "ou_1" {
		t.Fatalf("decoded payload mismatch: %+v", got)
	}
}

func TestSendHelpers_UsageSocketPathAndMimeDetection(t *testing.T) {
	stdout, stderr := captureP8CommandOutput(t, func() {
		runSend([]string{"--help"})
	})
	if stderr != "" {
		t.Fatalf("runSend help stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, "Usage: cc-connect send") || !strings.Contains(stdout, "--stdin") {
		t.Fatalf("runSend help stdout missing usage:\n%s", stdout)
	}

	dataDir := t.TempDir()
	if got := resolveSocketPath(dataDir); got != filepath.Join(dataDir, "run", "api.sock") {
		t.Fatalf("resolveSocketPath flag = %q, want data-dir socket", got)
	}
	envDataDir := t.TempDir()
	t.Setenv("CC_DATA_DIR", envDataDir)
	if got := resolveSocketPath(""); got != filepath.Join(envDataDir, "run", "api.sock") {
		t.Fatalf("resolveSocketPath env = %q, want env socket", got)
	}

	tests := []struct {
		name     string
		fileName string
		data     []byte
		want     string
	}{
		{name: "markdown extension", fileName: "readme.md", data: []byte("# title"), want: "text/markdown"},
		{name: "empty unknown", fileName: "blob.unknown", data: nil, want: "application/octet-stream"},
		{name: "sniff html", fileName: "blob", data: []byte("<html><body>hi</body></html>"), want: "text/html"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectAttachmentMimeType(tt.fileName, tt.data)
			if !strings.HasPrefix(got, tt.want) {
				t.Fatalf("detectAttachmentMimeType(%q) = %q, want prefix %q", tt.fileName, got, tt.want)
			}
		})
	}

	if !attachmentMatchesMediaType("application/octet-stream", "voice.wav", "audio") {
		t.Fatalf("wav extension should be accepted as audio")
	}
	if !attachmentMatchesMediaType("application/octet-stream", "movie.webm", "video") {
		t.Fatalf("webm extension should be accepted as video")
	}
	if attachmentMatchesMediaType("text/plain", "notes.txt", "audio") {
		t.Fatalf("text file should not be accepted as audio")
	}
}

func TestRunSend_UsageAndParseErrorsExit(t *testing.T) {
	result := runCommandHelper(t, "send")
	if result.code == 0 || !strings.Contains(result.stderr, "message, tts text, or attachment is required") || !strings.Contains(result.stdout, "Usage: cc-connect send") {
		t.Fatalf("send missing message result = %+v, want usage failure", result)
	}

	result = runCommandHelper(t, "send", "--project")
	if result.code == 0 || !strings.Contains(result.stderr, "--project requires a value") {
		t.Fatalf("send parse error result = %+v, want project value failure", result)
	}
}

func withStdin(t *testing.T, input string, fn func()) {
	t.Helper()
	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	if _, err := w.WriteString(input); err != nil {
		t.Fatalf("write stdin: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close stdin writer: %v", err)
	}
	os.Stdin = r
	defer func() {
		os.Stdin = oldStdin
		_ = r.Close()
	}()

	fn()
}

type p8CommandResult struct {
	stdout string
	stderr string
	code   int
}

func runCommandHelper(t *testing.T, command string, args ...string) p8CommandResult {
	t.Helper()
	cmdArgs := append([]string{"-test.run=TestP8CommandHelperProcess", "--", command}, args...)
	cmd := exec.Command(os.Args[0], cmdArgs...)
	cmd.Env = append(os.Environ(), "CC_CONNECT_P8_HELPER=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := p8CommandResult{stdout: stdout.String(), stderr: stderr.String()}
	if exitErr, ok := err.(*exec.ExitError); ok {
		result.code = exitErr.ExitCode()
		return result
	}
	if err != nil {
		t.Fatalf("run helper command: %v", err)
	}
	return result
}

func runSessionsCommandHelper(t *testing.T, args ...string) p8CommandResult {
	t.Helper()
	return runCommandHelper(t, "sessions", args...)
}

func TestP8CommandHelperProcess(t *testing.T) {
	if os.Getenv("CC_CONNECT_P8_HELPER") != "1" {
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
	case "send":
		runSend(args)
	case "relay":
		runRelay(args)
	case "sessions":
		runSessions(args)
	default:
		os.Exit(2)
	}
	os.Exit(0)
}
