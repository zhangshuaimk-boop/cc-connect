package core

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebhookServer_AuthBearer(t *testing.T) {
	ws := NewWebhookServer(0, "my-secret", "/hook")
	r := httptest.NewRequest(http.MethodPost, "/hook", nil)
	r.Header.Set("Authorization", "Bearer my-secret")
	if !ws.authenticate(r) {
		t.Error("expected auth to succeed with correct Bearer token")
	}
	r.Header.Set("Authorization", "Bearer wrong")
	if ws.authenticate(r) {
		t.Error("expected auth to fail with wrong Bearer token")
	}
}

func TestWebhookServer_AuthHeader(t *testing.T) {
	ws := NewWebhookServer(0, "tok123", "/hook")
	r := httptest.NewRequest(http.MethodPost, "/hook", nil)
	r.Header.Set("X-Webhook-Token", "tok123")
	if !ws.authenticate(r) {
		t.Error("expected auth to succeed with X-Webhook-Token")
	}
}

func TestWebhookServer_AuthQuery(t *testing.T) {
	ws := NewWebhookServer(0, "qsecret", "/hook")
	r := httptest.NewRequest(http.MethodPost, "/hook?token=qsecret", nil)
	if !ws.authenticate(r) {
		t.Error("expected auth to succeed with query token")
	}
}

func TestWebhookServer_NoTokenRequired(t *testing.T) {
	ws := NewWebhookServer(0, "", "/hook")
	r := httptest.NewRequest(http.MethodPost, "/hook", nil)
	if !ws.authenticate(r) {
		t.Error("expected auth to pass when no token configured")
	}
}

func TestWebhookServer_HandleHook_MethodNotAllowed(t *testing.T) {
	ws := NewWebhookServer(0, "", "/hook")
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/hook", nil)
	ws.handleHook(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestWebhookServer_HandleHook_Unauthorized(t *testing.T) {
	ws := NewWebhookServer(0, "secret", "/hook")
	w := httptest.NewRecorder()
	body, _ := json.Marshal(WebhookRequest{SessionKey: "tg:1:1", Prompt: "hi"})
	r := httptest.NewRequest(http.MethodPost, "/hook", bytes.NewReader(body))
	ws.handleHook(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestWebhookServer_HandleHook_MalformedJSON(t *testing.T) {
	ws := NewWebhookServer(0, "", "/hook")
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/hook", strings.NewReader("{bad"))

	ws.handleHook(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "invalid JSON") {
		t.Fatalf("body = %q, want invalid JSON", w.Body.String())
	}
}

func TestWebhookServer_HandleHook_Validation(t *testing.T) {
	ws := NewWebhookServer(0, "", "/hook")

	tests := []struct {
		name string
		body WebhookRequest
		code int
	}{
		{"missing session_key", WebhookRequest{Prompt: "hi"}, http.StatusBadRequest},
		{"missing prompt and exec", WebhookRequest{SessionKey: "tg:1:1"}, http.StatusBadRequest},
		{"both prompt and exec", WebhookRequest{SessionKey: "tg:1:1", Prompt: "hi", Exec: "ls"}, http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			body, _ := json.Marshal(tt.body)
			r := httptest.NewRequest(http.MethodPost, "/hook", bytes.NewReader(body))
			ws.handleHook(w, r)
			if w.Code != tt.code {
				t.Errorf("expected %d, got %d: %s", tt.code, w.Code, w.Body.String())
			}
		})
	}
}

func TestWebhookServer_ResolveEngineBoundaries(t *testing.T) {
	ws := NewWebhookServer(0, "", "/hook")
	engineA := NewEngine("a", &stubAgent{}, nil, "", LangEnglish)
	engineB := NewEngine("b", &stubAgent{}, nil, "", LangEnglish)
	ws.RegisterEngine("a", engineA)

	got, err := ws.resolveEngine("")
	if err != nil {
		t.Fatalf("resolve single default: %v", err)
	}
	if got != engineA {
		t.Fatalf("resolve single default returned %#v, want engineA", got)
	}

	got, err = ws.resolveEngine("a")
	if err != nil {
		t.Fatalf("resolve named engine: %v", err)
	}
	if got != engineA {
		t.Fatalf("resolve named engine returned %#v, want engineA", got)
	}

	if _, err := ws.resolveEngine("missing"); err == nil || !strings.Contains(err.Error(), `project "missing" not found`) {
		t.Fatalf("resolve missing err = %v, want not found", err)
	}

	ws.RegisterEngine("b", engineB)
	if _, err := ws.resolveEngine(""); err == nil || !strings.Contains(err.Error(), "project is required") {
		t.Fatalf("resolve ambiguous err = %v, want project required", err)
	}
}

func TestWebhookServer_HandleHook_AcceptsPromptRequest(t *testing.T) {
	ws := NewWebhookServer(0, "", "/hook")
	ws.RegisterEngine("project", NewEngine("project", &stubAgent{}, nil, "", LangEnglish))

	body, err := json.Marshal(WebhookRequest{
		Event:      "ci:passed",
		Project:    "project",
		SessionKey: "feishu:chat:user",
		Prompt:     "summarize build",
		Payload:    map[string]any{"build": 123},
		Silent:     true,
	})
	if err != nil {
		t.Fatalf("marshal webhook request: %v", err)
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/hook", bytes.NewReader(body))

	ws.handleHook(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["status"] != "accepted" || resp["event"] != "ci:passed" {
		t.Fatalf("response = %#v, want accepted ci:passed", resp)
	}
}

func TestWebhookServer_DefaultValues(t *testing.T) {
	ws := NewWebhookServer(0, "", "")
	if ws.port != 9111 {
		t.Errorf("expected default port 9111, got %d", ws.port)
	}
	if ws.path != "/hook" {
		t.Errorf("expected default path /hook, got %s", ws.path)
	}
}
