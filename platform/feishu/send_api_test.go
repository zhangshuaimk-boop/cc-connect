package feishu

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chenhg5/cc-connect/core"
	lark "github.com/larksuite/oapi-sdk-go/v3"
)

func newSendAPITestPlatform(appID, appSecret, baseURL string, client *http.Client) *Platform {
	return &Platform{
		platformName: "feishu",
		domain:       baseURL,
		appID:        appID,
		appSecret:    appSecret,
		client: lark.NewClient(appID, appSecret,
			lark.WithOpenBaseUrl(baseURL),
			lark.WithHttpClient(client),
		),
		replayClient: lark.NewClient(appID, appSecret,
			lark.WithEnableTokenCache(false),
			lark.WithOpenBaseUrl(baseURL),
			lark.WithHttpClient(client),
		),
	}
}

func sendAPITestAuthHandler(t *testing.T, w http.ResponseWriter, token string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	writeJSON(t, w, map[string]any{
		"code":                0,
		"msg":                 "success",
		"expire":              7200,
		"tenant_access_token": token,
	})
}

func TestSendCreatesTextMessage(t *testing.T) {
	const appID = "send_text_app"
	const appSecret = "send_text_secret"

	createCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal":
			sendAPITestAuthHandler(t, w, "send-token")
		case r.URL.Path == "/open-apis/im/v1/messages":
			createCalls++
			if got := r.Header.Get("Authorization"); got != "Bearer send-token" {
				t.Fatalf("Authorization = %q, want send-token", got)
			}
			if got := r.URL.Query().Get("receive_id_type"); got != "chat_id" {
				t.Fatalf("receive_id_type = %q, want chat_id", got)
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode request body: %v", err)
			}
			if body["receive_id"] != "oc_chat" || body["msg_type"] != "text" {
				t.Fatalf("unexpected create body: %#v", body)
			}
			if !strings.Contains(body["content"].(string), "hello") {
				t.Fatalf("content = %q, want hello", body["content"])
			}
			writeJSON(t, w, map[string]any{
				"code": 0,
				"msg":  "success",
				"data": map[string]any{"message_id": "om_text"},
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	p := newSendAPITestPlatform(appID, appSecret, srv.URL, srv.Client())
	if err := p.Send(context.Background(), replyContext{chatID: "oc_chat"}, "hello"); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if createCalls != 1 {
		t.Fatalf("createCalls = %d, want 1", createCalls)
	}
}

func TestSendCardCreatesInteractiveMessage(t *testing.T) {
	const appID = "send_card_app"
	const appSecret = "send_card_secret"

	createCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal":
			sendAPITestAuthHandler(t, w, "card-token")
		case r.URL.Path == "/open-apis/im/v1/messages":
			createCalls++
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode request body: %v", err)
			}
			if body["receive_id"] != "oc_chat" || body["msg_type"] != "interactive" {
				t.Fatalf("unexpected card create body: %#v", body)
			}
			content := body["content"].(string)
			if !strings.Contains(content, "deploy ready") {
				t.Fatalf("card content = %q, want rendered card title", content)
			}
			writeJSON(t, w, map[string]any{
				"code": 0,
				"msg":  "success",
				"data": map[string]any{"message_id": "om_card"},
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	p := newSendAPITestPlatform(appID, appSecret, srv.URL, srv.Client())
	ip := &interactivePlatform{Platform: p}
	err := ip.SendCard(context.Background(), replyContext{chatID: "oc_chat", sessionKey: "oc_chat"}, &core.Card{
		Header: &core.CardHeader{Title: "deploy ready"},
	})
	if err != nil {
		t.Fatalf("SendCard() error = %v", err)
	}
	if createCalls != 1 {
		t.Fatalf("createCalls = %d, want 1", createCalls)
	}
}

func TestSendImageUploadsAndRepliesWithImageKey(t *testing.T) {
	const appID = "send_image_app"
	const appSecret = "send_image_secret"

	uploadCalls := 0
	replyCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal":
			sendAPITestAuthHandler(t, w, "image-token")
		case r.URL.Path == "/open-apis/im/v1/images":
			uploadCalls++
			if got := r.Header.Get("Authorization"); got != "Bearer image-token" {
				t.Fatalf("image upload Authorization = %q, want image-token", got)
			}
			data, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read upload body: %v", err)
			}
			if !strings.Contains(string(data), "image-bytes") {
				t.Fatalf("upload body does not contain image payload: %q", string(data))
			}
			writeJSON(t, w, map[string]any{
				"code": 0,
				"msg":  "success",
				"data": map[string]any{"image_key": "img_v2_key"},
			})
		case strings.HasSuffix(r.URL.Path, "/reply"):
			replyCalls++
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode reply body: %v", err)
			}
			if body["msg_type"] != "image" || !strings.Contains(body["content"].(string), "img_v2_key") {
				t.Fatalf("unexpected image reply body: %#v", body)
			}
			writeJSON(t, w, map[string]any{
				"code": 0,
				"msg":  "success",
				"data": map[string]any{"message_id": "om_image_reply"},
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	p := newSendAPITestPlatform(appID, appSecret, srv.URL, srv.Client())
	err := p.SendImage(context.Background(), replyContext{messageID: "om_root", chatID: "oc_chat"}, core.ImageAttachment{
		Data: []byte("image-bytes"),
	})
	if err != nil {
		t.Fatalf("SendImage() error = %v", err)
	}
	if uploadCalls != 1 || replyCalls != 1 {
		t.Fatalf("uploadCalls=%d replyCalls=%d, want 1 each", uploadCalls, replyCalls)
	}
}

func TestSendFileUploadsAndCreatesFileMessage(t *testing.T) {
	const appID = "send_file_app"
	const appSecret = "send_file_secret"

	uploadCalls := 0
	createCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal":
			sendAPITestAuthHandler(t, w, "file-token")
		case r.URL.Path == "/open-apis/im/v1/files":
			uploadCalls++
			data, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read file upload body: %v", err)
			}
			raw := string(data)
			if !strings.Contains(raw, "report.pdf") || !strings.Contains(raw, "file-bytes") {
				t.Fatalf("file upload body = %q, want file name and payload", raw)
			}
			writeJSON(t, w, map[string]any{
				"code": 0,
				"msg":  "success",
				"data": map[string]any{"file_key": "file_v2_key"},
			})
		case r.URL.Path == "/open-apis/im/v1/messages":
			createCalls++
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode create body: %v", err)
			}
			if body["msg_type"] != "file" || !strings.Contains(body["content"].(string), "file_v2_key") {
				t.Fatalf("unexpected file create body: %#v", body)
			}
			writeJSON(t, w, map[string]any{
				"code": 0,
				"msg":  "success",
				"data": map[string]any{"message_id": "om_file"},
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	p := newSendAPITestPlatform(appID, appSecret, srv.URL, srv.Client())
	err := p.SendFile(context.Background(), replyContext{chatID: "oc_chat"}, core.FileAttachment{
		FileName: "report.pdf",
		MimeType: "application/pdf",
		Data:     []byte("file-bytes"),
	})
	if err != nil {
		t.Fatalf("SendFile() error = %v", err)
	}
	if uploadCalls != 1 || createCalls != 1 {
		t.Fatalf("uploadCalls=%d createCalls=%d, want 1 each", uploadCalls, createCalls)
	}
}

func TestSendAPIUploadImageRefreshesTenantToken(t *testing.T) {
	const appID = "upload_refresh_app"
	const appSecret = "upload_refresh_secret"

	authCalls := 0
	uploadCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal":
			authCalls++
			token := "stale-upload-token"
			if authCalls >= 2 {
				token = "fresh-upload-token"
			}
			sendAPITestAuthHandler(t, w, token)
		case r.URL.Path == "/open-apis/im/v1/images":
			uploadCalls++
			switch uploadCalls {
			case 1, 2:
				if got := r.Header.Get("Authorization"); got != "Bearer stale-upload-token" {
					t.Fatalf("upload call %d Authorization = %q, want stale token", uploadCalls, got)
				}
				writeJSON(t, w, map[string]any{
					"code": 99991663,
					"msg":  "Invalid access token for authorization",
				})
			case 3:
				if got := r.Header.Get("Authorization"); got != "Bearer fresh-upload-token" {
					t.Fatalf("upload retry Authorization = %q, want fresh token", got)
				}
				writeJSON(t, w, map[string]any{
					"code": 0,
					"msg":  "success",
					"data": map[string]any{"image_key": "fresh_image_key"},
				})
			default:
				t.Fatalf("unexpected upload call %d", uploadCalls)
			}
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	p := newSendAPITestPlatform(appID, appSecret, srv.URL, srv.Client())
	key, err := p.sendAPI().uploadImageKey(context.Background(), []byte("image"))
	if err != nil {
		t.Fatalf("uploadImageKey() error = %v", err)
	}
	if key != "fresh_image_key" {
		t.Fatalf("uploadImageKey() = %q, want fresh_image_key", key)
	}
	if authCalls != 2 || uploadCalls != 3 {
		t.Fatalf("authCalls=%d uploadCalls=%d, want 2 and 3", authCalls, uploadCalls)
	}
}

func TestSendAPIUploadFileReturnsAPIError(t *testing.T) {
	const appID = "upload_file_error_app"
	const appSecret = "upload_file_error_secret"

	uploadCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal":
			sendAPITestAuthHandler(t, w, "file-error-token")
		case r.URL.Path == "/open-apis/im/v1/files":
			uploadCalls++
			writeJSON(t, w, map[string]any{
				"code": 230001,
				"msg":  "rate limited",
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	p := newSendAPITestPlatform(appID, appSecret, srv.URL, srv.Client())
	_, err := p.sendAPI().uploadFileKey(context.Background(), "upload file", "stream", "clip.bin", []byte("data"))
	if err == nil {
		t.Fatal("uploadFileKey() error = nil, want API error")
	}
	if !strings.Contains(err.Error(), "rate limited") {
		t.Fatalf("uploadFileKey() error = %v, want rate limited", err)
	}
	if uploadCalls != 1 {
		t.Fatalf("uploadCalls = %d, want 1", uploadCalls)
	}
}

func TestSendAPIPatchMessageOnceAndDownloadResource(t *testing.T) {
	const appID = "patch_download_app"
	const appSecret = "patch_download_secret"

	patchCalls := 0
	downloadCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal":
			sendAPITestAuthHandler(t, w, "patch-download-token")
		case strings.Contains(r.URL.Path, "/messages/") && r.Method == http.MethodPatch:
			patchCalls++
			writeJSON(t, w, map[string]any{"code": 0, "msg": "success"})
		case strings.Contains(r.URL.Path, "/messages/") && strings.Contains(r.URL.Path, "/resources/"):
			downloadCalls++
			w.Header().Set("Content-Type", "application/octet-stream")
			w.WriteHeader(http.StatusOK)
			if _, err := w.Write([]byte("resource-bytes")); err != nil {
				t.Fatalf("write resource: %v", err)
			}
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	p := newSendAPITestPlatform(appID, appSecret, srv.URL, srv.Client())
	if err := p.sendAPI().patchMessageOnce(context.Background(), "om_card", `{"card":true}`, feishuMessageAPILabels{
		network: "patch once",
		failed:  "patch once failed",
	}); err != nil {
		t.Fatalf("patchMessageOnce() error = %v", err)
	}
	data, err := p.sendAPI().downloadMessageResource(context.Background(), "om_root", "file_key", "file", "resource API")
	if err != nil {
		t.Fatalf("downloadMessageResource() error = %v", err)
	}
	if string(data) != "resource-bytes" {
		t.Fatalf("downloadMessageResource() = %q, want resource-bytes", string(data))
	}
	if patchCalls != 1 || downloadCalls != 1 {
		t.Fatalf("patchCalls=%d downloadCalls=%d, want 1 each", patchCalls, downloadCalls)
	}
}

func TestDownloadMessageResourceErrorsAndReadName(t *testing.T) {
	if got := resourceReadName("image API"); got != "image" {
		t.Fatalf("resourceReadName(image API) = %q, want image", got)
	}
	if got := resourceReadName("resource API"); got != "resource" {
		t.Fatalf("resourceReadName(resource API) = %q, want resource", got)
	}
	if got := resourceReadName("custom op"); got != "custom op" {
		t.Fatalf("resourceReadName(custom op) = %q, want custom op", got)
	}

	const appID = "download_error_app"
	const appSecret = "download_error_secret"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal":
			sendAPITestAuthHandler(t, w, "download-error-token")
		case strings.Contains(r.URL.Path, "/messages/") && strings.Contains(r.URL.Path, "/resources/"):
			w.WriteHeader(http.StatusTooManyRequests)
			writeJSON(t, w, map[string]any{
				"code": 230001,
				"msg":  "rate limited",
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	p := newSendAPITestPlatform(appID, appSecret, srv.URL, srv.Client())
	_, err := p.sendAPI().downloadMessageResource(context.Background(), "om_root", "file_key", "file", "resource API")
	if err == nil {
		t.Fatal("downloadMessageResource() error = nil, want API error")
	}
	if !strings.Contains(err.Error(), "rate limited") {
		t.Fatalf("downloadMessageResource() error = %v, want rate limited", err)
	}
}
