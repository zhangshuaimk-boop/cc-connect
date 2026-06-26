package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestRelaySend_PostsPayloadToUnixAPI(t *testing.T) {
	dataDir, requests := startCommandAPIServer(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		if r.URL.Path != "/relay/send" {
			http.NotFound(w, r)
			return
		}
		for key, want := range map[string]any{
			"from":        "source-project",
			"to":          "target-project",
			"session_key": "feishu:chat:user",
			"message":     "hello relay",
		} {
			if body[key] != want {
				http.Error(w, "wrong relay payload", http.StatusBadRequest)
				return
			}
		}
		writeJSONResponse(t, w, http.StatusOK, map[string]string{"response": "relay response"})
	})

	stdout, stderr := captureP8CommandOutput(t, func() {
		runRelay([]string{
			"send",
			"--data-dir", dataDir,
			"--from", "source-project",
			"--to", "target-project",
			"--session-key", "feishu:chat:user",
			"--message", "hello relay",
		})
	})
	if stderr != "" {
		t.Fatalf("runRelay stderr = %q, want empty", stderr)
	}
	if stdout != "relay response" {
		t.Fatalf("runRelay stdout = %q, want relay response", stdout)
	}
	req := nextCommandAPIRequest(t, requests)
	if req.Method != http.MethodPost || req.Path != "/relay/send" {
		t.Fatalf("relay request = %s %s, want POST /relay/send", req.Method, req.Path)
	}
	for key, want := range map[string]any{
		"from":        "source-project",
		"to":          "target-project",
		"session_key": "feishu:chat:user",
		"message":     "hello relay",
	} {
		if got := req.Body[key]; got != want {
			t.Fatalf("relay body[%s] = %#v, want %#v; body=%v", key, got, want, req.Body)
		}
	}
}

func TestRelaySend_PositionalArgsAndEnvContext(t *testing.T) {
	t.Setenv("CC_PROJECT", "env-source")
	t.Setenv("CC_SESSION_KEY", "env-session")
	dataDir, requests := startCommandAPIServer(t, func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
		writeJSONResponse(t, w, http.StatusOK, map[string]string{"response": "ok"})
	})

	stdout, stderr := captureP8CommandOutput(t, func() {
		runRelay([]string{"send", "--data-dir", dataDir, "target-bot", "hello", "from", "positionals"})
	})
	if stderr != "" {
		t.Fatalf("runRelay positional stderr = %q, want empty", stderr)
	}
	if stdout != "ok" {
		t.Fatalf("runRelay positional stdout = %q, want ok", stdout)
	}
	req := nextCommandAPIRequest(t, requests)
	for key, want := range map[string]any{
		"from":        "env-source",
		"to":          "target-bot",
		"session_key": "env-session",
		"message":     "hello from positionals",
	} {
		if got := req.Body[key]; got != want {
			t.Fatalf("relay positional body[%s] = %#v, want %#v; body=%v", key, got, want, req.Body)
		}
	}
}

func TestRelaySend_ErrorReturnsExit(t *testing.T) {
	t.Setenv("CC_PROJECT", "")
	t.Setenv("CC_SESSION_KEY", "")
	dataDir, _ := startCommandAPIServer(t, func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
		http.Error(w, "target unavailable", http.StatusBadGateway)
	})

	result := runCommandHelper(t, "relay", "send", "--data-dir", dataDir, "--to", "target", "--session-key", "feishu:chat:user", "-m", "hello")
	if result.code == 0 || !strings.Contains(result.stderr, "target unavailable") {
		t.Fatalf("relay server failure result = %+v, want target unavailable", result)
	}

	result = runCommandHelper(t, "relay", "send", "--data-dir", t.TempDir(), "--to", "target", "--session-key", "feishu:chat:user", "-m", "hello")
	if result.code == 0 || !strings.Contains(result.stderr, "socket not found") {
		t.Fatalf("relay missing socket result = %+v, want socket not found", result)
	}

	result = runCommandHelper(t, "relay", "send", "--data-dir", dataDir, "--session-key", "feishu:chat:user", "-m", "hello")
	if result.code == 0 || !strings.Contains(result.stderr, "target project") {
		t.Fatalf("relay missing target result = %+v, want target project failure", result)
	}

	result = runCommandHelper(t, "relay", "send", "--data-dir", dataDir, "--to", "target", "-m", "hello")
	if result.code == 0 || !strings.Contains(result.stderr, "session key is required") {
		t.Fatalf("relay missing session result = %+v, want session key failure", result)
	}

	result = runCommandHelper(t, "relay", "unknown")
	if result.code == 0 || !strings.Contains(result.stderr, "Unknown relay subcommand") {
		t.Fatalf("relay unknown subcommand result = %+v, want unknown subcommand failure", result)
	}
}

func TestRelaySend_DecodeResponseErrorExits(t *testing.T) {
	dataDir, _ := startCommandAPIServer(t, func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{bad-json"))
	})

	result := runCommandHelper(t, "relay", "send", "--data-dir", dataDir, "--to", "target", "--session-key", "feishu:chat:user", "-m", "hello")
	if result.code == 0 || !strings.Contains(result.stderr, "decode response") {
		t.Fatalf("relay decode failure result = %+v, want decode response failure", result)
	}
}

func TestRelayUsageBranches(t *testing.T) {
	stdout, stderr := captureP8CommandOutput(t, func() {
		runRelay(nil)
	})
	if stderr != "" {
		t.Fatalf("relay empty stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, "Usage: cc-connect relay") {
		t.Fatalf("relay empty stdout missing usage:\n%s", stdout)
	}

	stdout, stderr = captureP8CommandOutput(t, func() {
		runRelay([]string{"send", "--help"})
	})
	if stderr != "" {
		t.Fatalf("relay send help stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, "Usage: cc-connect relay send") || !strings.Contains(stdout, "--session-key") {
		t.Fatalf("relay send help stdout missing usage:\n%s", stdout)
	}
}

func TestRelaySendPayloadShape(t *testing.T) {
	dataDir, requests := startCommandAPIServer(t, func(w http.ResponseWriter, _ *http.Request, body map[string]any) {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		if !strings.Contains(string(raw), `"session_key":"s1"`) {
			t.Fatalf("relay raw body missing session key: %s", raw)
		}
		writeJSONResponse(t, w, http.StatusOK, map[string]string{"response": ""})
	})

	_, stderr := captureP8CommandOutput(t, func() {
		runRelay([]string{"send", "--data-dir", dataDir, "--from", "a", "--to", "b", "--session-key", "s1", "--message", "m"})
	})
	if stderr != "" {
		t.Fatalf("runRelay stderr = %q, want empty", stderr)
	}
	_ = nextCommandAPIRequest(t, requests)
}
