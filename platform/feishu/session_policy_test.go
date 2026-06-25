package feishu

import (
	"testing"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func TestFeishuSessionPolicySessionKeyForMessage(t *testing.T) {
	const (
		chatID = "oc_chat"
		userID = "ou_user"
	)

	tests := []struct {
		name   string
		policy feishuSessionPolicy
		msg    *larkim.EventMessage
		want   string
	}{
		{
			name: "group message keeps legacy user key without thread isolation",
			policy: feishuSessionPolicy{
				platformName: "feishu",
			},
			msg: &larkim.EventMessage{
				ChatType:  stringPtr("group"),
				MessageId: stringPtr("om_root"),
			},
			want: "feishu:oc_chat:ou_user",
		},
		{
			name: "group root opens thread isolated session",
			policy: feishuSessionPolicy{
				platformName:    "feishu",
				threadIsolation: true,
			},
			msg: &larkim.EventMessage{
				ChatType:  stringPtr("group"),
				MessageId: stringPtr("om_root"),
			},
			want: "feishu:oc_chat:root:om_root",
		},
		{
			name: "group topic reply uses root id",
			policy: feishuSessionPolicy{
				platformName:    "feishu",
				threadIsolation: true,
			},
			msg: &larkim.EventMessage{
				ChatType:  stringPtr("group"),
				MessageId: stringPtr("om_child"),
				RootId:    stringPtr("om_root"),
				ThreadId:  stringPtr("omt_thread"),
			},
			want: "feishu:oc_chat:root:om_root",
		},
		{
			name: "p2p ignores thread isolation",
			policy: feishuSessionPolicy{
				platformName:    "feishu",
				threadIsolation: true,
			},
			msg: &larkim.EventMessage{
				ChatType:  stringPtr("p2p"),
				MessageId: stringPtr("om_direct"),
			},
			want: "feishu:oc_chat:ou_user",
		},
		{
			name: "shared channel key stays two-part",
			policy: feishuSessionPolicy{
				platformName:          "feishu",
				shareSessionInChannel: true,
			},
			msg: &larkim.EventMessage{
				ChatType:  stringPtr("group"),
				MessageId: stringPtr("om_root"),
			},
			want: "feishu:oc_chat",
		},
		{
			name: "thread isolation takes precedence over shared channel for group thread",
			policy: feishuSessionPolicy{
				platformName:          "feishu",
				shareSessionInChannel: true,
				threadIsolation:       true,
			},
			msg: &larkim.EventMessage{
				ChatType:  stringPtr("group"),
				MessageId: stringPtr("om_root"),
			},
			want: "feishu:oc_chat:root:om_root",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.policy.sessionKeyForMessage(tt.msg, chatID, userID); got != tt.want {
				t.Fatalf("sessionKeyForMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFeishuSessionPolicySessionKeyFromCardAction(t *testing.T) {
	tests := []struct {
		name   string
		policy feishuSessionPolicy
		value  map[string]any
		want   string
	}{
		{
			name:   "card action preserves embedded thread session key",
			policy: feishuSessionPolicy{platformName: "feishu", threadIsolation: true},
			value:  map[string]any{"session_key": "feishu:oc_chat:root:om_root"},
			want:   "feishu:oc_chat:root:om_root",
		},
		{
			name:   "card action falls back to user scoped key",
			policy: feishuSessionPolicy{platformName: "feishu"},
			value:  map[string]any{"action": "cmd:/help"},
			want:   "feishu:oc_chat:ou_user",
		},
		{
			name:   "card action fallback honors shared channel sessions",
			policy: feishuSessionPolicy{platformName: "feishu", shareSessionInChannel: true},
			value:  nil,
			want:   "feishu:oc_chat",
		},
		{
			name:   "card action empty embedded key falls back",
			policy: feishuSessionPolicy{platformName: "feishu"},
			value:  map[string]any{"session_key": ""},
			want:   "feishu:oc_chat:ou_user",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.policy.sessionKeyFromCardAction("oc_chat", "ou_user", tt.value); got != tt.want {
				t.Fatalf("sessionKeyFromCardAction() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFeishuSessionPolicyReplyTargetDecisions(t *testing.T) {
	tests := []struct {
		name              string
		policy            feishuSessionPolicy
		rc                replyContext
		wantReplyAPI      bool
		wantReplyInThread bool
	}{
		{
			name:              "thread session uses reply api and reply in thread",
			policy:            feishuSessionPolicy{platformName: "feishu", threadIsolation: true},
			rc:                replyContext{messageID: "om_child", chatID: "oc_chat", sessionKey: "feishu:oc_chat:root:om_root"},
			wantReplyAPI:      true,
			wantReplyInThread: true,
		},
		{
			name:              "thread key without message id cannot use reply api",
			policy:            feishuSessionPolicy{platformName: "feishu", threadIsolation: true},
			rc:                replyContext{chatID: "oc_chat", sessionKey: "feishu:oc_chat:root:om_root"},
			wantReplyAPI:      false,
			wantReplyInThread: false,
		},
		{
			name:              "user scoped group message replies without thread flag",
			policy:            feishuSessionPolicy{platformName: "feishu", threadIsolation: true},
			rc:                replyContext{messageID: "om_msg", chatID: "oc_chat", sessionKey: "feishu:oc_chat:ou_user"},
			wantReplyAPI:      true,
			wantReplyInThread: false,
		},
		{
			name:              "p2p message uses reply api without thread flag",
			policy:            feishuSessionPolicy{platformName: "feishu", threadIsolation: true},
			rc:                replyContext{messageID: "om_p2p", chatID: "ou_user", sessionKey: "feishu:ou_user:ou_user"},
			wantReplyAPI:      true,
			wantReplyInThread: false,
		},
		{
			name:              "no reply to trigger forces create API",
			policy:            feishuSessionPolicy{platformName: "feishu", threadIsolation: true, noReplyToTrigger: true},
			rc:                replyContext{messageID: "om_child", chatID: "oc_chat", sessionKey: "feishu:oc_chat:root:om_root"},
			wantReplyAPI:      false,
			wantReplyInThread: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.policy.shouldUseThreadOrReplyAPI(tt.rc); got != tt.wantReplyAPI {
				t.Fatalf("shouldUseThreadOrReplyAPI() = %v, want %v", got, tt.wantReplyAPI)
			}
			if got := tt.policy.shouldReplyInThread(tt.rc); got != tt.wantReplyInThread {
				t.Fatalf("shouldReplyInThread() = %v, want %v", got, tt.wantReplyInThread)
			}
		})
	}
}

func TestFeishuSessionPolicyReconstructReplyCtx(t *testing.T) {
	policy := feishuSessionPolicy{platformName: "feishu"}

	tests := []struct {
		name          string
		sessionKey    string
		wantChatID    string
		wantMessageID string
		wantErr       bool
	}{
		{
			name:       "user scoped key",
			sessionKey: "feishu:oc_chat:ou_user",
			wantChatID: "oc_chat",
		},
		{
			name:          "root thread key",
			sessionKey:    "feishu:oc_chat:root:om_root",
			wantChatID:    "oc_chat",
			wantMessageID: "om_root",
		},
		{
			name:          "legacy thread prefix key",
			sessionKey:    "feishu:oc_chat:thread:omt_thread",
			wantChatID:    "oc_chat",
			wantMessageID: "omt_thread",
		},
		{
			name:       "shared channel key",
			sessionKey: "feishu:oc_chat",
			wantChatID: "oc_chat",
		},
		{
			name:       "foreign platform rejected",
			sessionKey: "lark:oc_chat:ou_user",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := policy.reconstructReplyCtx(tt.sessionKey)
			if tt.wantErr {
				if err == nil {
					t.Fatal("reconstructReplyCtx() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("reconstructReplyCtx() error = %v", err)
			}
			if got.chatID != tt.wantChatID {
				t.Fatalf("chatID = %q, want %q", got.chatID, tt.wantChatID)
			}
			if got.messageID != tt.wantMessageID {
				t.Fatalf("messageID = %q, want %q", got.messageID, tt.wantMessageID)
			}
			if got.sessionKey != tt.sessionKey {
				t.Fatalf("sessionKey = %q, want %q", got.sessionKey, tt.sessionKey)
			}
		})
	}
}

func TestFeishuSessionPolicyRelayGroupVisibilityKey(t *testing.T) {
	tests := []struct {
		name       string
		sessionKey string
		wantKey    string
		wantOK     bool
	}{
		{"root thread", "feishu:oc_chat:root:om_root", "feishu:oc_chat:root:om_root", true},
		{"legacy thread prefix", "feishu:oc_chat:thread:omt_thread", "feishu:oc_chat:thread:omt_thread", true},
		{"user scoped group", "feishu:oc_chat:ou_user", "", false},
		{"p2p", "feishu:ou_user:ou_user", "", false},
		{"shared channel", "feishu:oc_chat", "", false},
		{"foreign root", "lark:oc_chat:root:om_root", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotKey, gotOK := relayGroupVisibilityKey(tt.sessionKey)
			if gotKey != tt.wantKey || gotOK != tt.wantOK {
				t.Fatalf("relayGroupVisibilityKey() = (%q, %v), want (%q, %v)", gotKey, gotOK, tt.wantKey, tt.wantOK)
			}
		})
	}
}
