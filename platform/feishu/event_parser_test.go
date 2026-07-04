package feishu

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/larksuite/oapi-sdk-go/v3/event"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func TestParseFeishuInboundMessageExtractsCoreInputs(t *testing.T) {
	chatType := "group"
	msgType := "text"
	senderType := "user"
	createTime := "1710000000123"
	content := `{"text":"@_user_1 hello"}`
	mention := &larkim.MentionEvent{
		Key:  stringPtr("@_user_1"),
		Id:   &larkim.UserId{OpenId: stringPtr("ou_bot")},
		Name: stringPtr("bot"),
	}

	got := parseFeishuInboundMessage(&larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{
				SenderId:   &larkim.UserId{OpenId: stringPtr("ou_user")},
				SenderType: &senderType,
			},
			Message: &larkim.EventMessage{
				MessageId:   stringPtr("om_msg"),
				ChatId:      stringPtr("oc_chat"),
				ChatType:    &chatType,
				MessageType: &msgType,
				Content:     &content,
				CreateTime:  &createTime,
				ParentId:    stringPtr("om_parent"),
				RootId:      stringPtr("om_root"),
				ThreadId:    stringPtr("omt_thread"),
				Mentions:    []*larkim.MentionEvent{mention},
			},
		},
	})

	if got.msgType != "text" || got.chatID != "oc_chat" || got.chatType != "group" {
		t.Fatalf("message route fields = type:%q chat:%q chatType:%q", got.msgType, got.chatID, got.chatType)
	}
	if got.userID != "ou_user" || got.messageID != "om_msg" {
		t.Fatalf("sender/message fields = user:%q message:%q", got.userID, got.messageID)
	}
	if got.senderType != "user" {
		t.Fatalf("senderType = %q, want user", got.senderType)
	}
	if !got.hasContent || got.content != content {
		t.Fatalf("content = %q has=%v, want raw content", got.content, got.hasContent)
	}
	if got.parentID != "om_parent" || got.rootID != "om_root" || got.threadID != "omt_thread" {
		t.Fatalf("thread fields = parent:%q root:%q thread:%q", got.parentID, got.rootID, got.threadID)
	}
	if got.createTimeMs != 1710000000123 {
		t.Fatalf("createTimeMs = %d", got.createTimeMs)
	}
	if len(got.mentions) != 1 || got.mentions[0] != mention {
		t.Fatalf("mentions were not preserved")
	}

	dispatch := got.dispatchInput("feishu:oc_chat:ou_user")
	if dispatch.content != content || dispatch.messageID != "om_msg" || dispatch.userID != "ou_user" || dispatch.senderType != "user" {
		t.Fatalf("dispatch input lost content/message/user: %#v", dispatch)
	}
	if dispatch.rctx != (replyContext{messageID: "om_msg", chatID: "oc_chat", sessionKey: "feishu:oc_chat:ou_user"}) {
		t.Fatalf("reply context = %#v", dispatch.rctx)
	}
}

func TestParseFeishuInboundMessageHandlesEmptyAndPartialEvents(t *testing.T) {
	for _, event := range []*larkim.P2MessageReceiveV1{
		nil,
		{},
		{Event: &larkim.P2MessageReceiveV1Data{}},
		{Event: &larkim.P2MessageReceiveV1Data{Sender: &larkim.EventSender{SenderId: &larkim.UserId{UserId: stringPtr("user_id_only")}}}},
	} {
		got := parseFeishuInboundMessage(event)
		if got.message != nil || got.hasContent || got.messageID != "" || got.chatID != "" {
			t.Fatalf("partial event parsed as non-empty message: %#v", got)
		}
	}
}

func TestFeishuEventDispatcherReplaysMessagePayloads(t *testing.T) {
	var got []feishuInboundMessage
	handler := dispatcher.NewEventDispatcher("", "").
		OnP2MessageReceiveV1(func(_ context.Context, event *larkim.P2MessageReceiveV1) error {
			got = append(got, parseFeishuInboundMessage(event))
			return nil
		})

	messagePayloads := []struct {
		name        string
		messageID   string
		msgType     string
		content     string
		mentions    []map[string]any
		wantContent bool
	}{
		{
			name:        "text with bot mention",
			messageID:   "om_text",
			msgType:     "text",
			content:     `{"text":"@_user_1 hello"}`,
			mentions:    []map[string]any{{"key": "@_user_1", "id": map[string]any{"open_id": "ou_bot"}, "name": "bot"}},
			wantContent: true,
		},
		{
			name:        "image attachment",
			messageID:   "om_image",
			msgType:     "image",
			content:     `{"image_key":"img_v2_abc"}`,
			wantContent: true,
		},
		{
			name:        "file attachment",
			messageID:   "om_file",
			msgType:     "file",
			content:     `{"file_key":"file_v2_abc","file_name":"report.txt"}`,
			wantContent: true,
		},
	}

	for _, payload := range messagePayloads {
		t.Run(payload.name, func(t *testing.T) {
			resp := handler.Handle(context.Background(), feishuEventReq(t, "im.message.receive_v1", map[string]any{
				"sender": map[string]any{
					"sender_id":   map[string]any{"open_id": "ou_user"},
					"sender_type": "user",
				},
				"message": map[string]any{
					"message_id":   payload.messageID,
					"root_id":      "om_root",
					"parent_id":    "om_parent",
					"thread_id":    "omt_thread",
					"chat_id":      "oc_chat",
					"chat_type":    "group",
					"message_type": payload.msgType,
					"content":      payload.content,
					"create_time":  "1710000000123",
					"mentions":     payload.mentions,
				},
			}))
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("Handle() status = %d body=%s", resp.StatusCode, string(resp.Body))
			}
		})
	}

	if len(got) != len(messagePayloads) {
		t.Fatalf("parsed messages = %d, want %d", len(got), len(messagePayloads))
	}
	for i, want := range messagePayloads {
		if got[i].messageID != want.messageID || got[i].msgType != want.msgType {
			t.Fatalf("message[%d] = id:%q type:%q, want id:%q type:%q", i, got[i].messageID, got[i].msgType, want.messageID, want.msgType)
		}
		if got[i].content != want.content || got[i].hasContent != want.wantContent {
			t.Fatalf("message[%d] content = %q has=%v, want %q has=%v", i, got[i].content, got[i].hasContent, want.content, want.wantContent)
		}
	}
	if len(got[0].mentions) != 1 || !isBotMentioned(got[0].mentions, "ou_bot") {
		t.Fatalf("text mention was not preserved: %#v", got[0].mentions)
	}
}

func TestFeishuEventDispatcherIgnoresReactionAndUnknownEvents(t *testing.T) {
	messageCalls := 0
	reactionCalls := 0
	handler := dispatcher.NewEventDispatcher("", "").
		OnP2MessageReceiveV1(func(_ context.Context, event *larkim.P2MessageReceiveV1) error {
			messageCalls++
			return nil
		}).
		OnP2MessageReactionCreatedV1(func(_ context.Context, event *larkim.P2MessageReactionCreatedV1) error {
			reactionCalls++
			return nil
		}).
		OnP2MessageReactionDeletedV1(func(_ context.Context, event *larkim.P2MessageReactionDeletedV1) error {
			reactionCalls++
			return nil
		})

	for _, eventType := range []string{"im.message.reaction.created_v1", "im.message.reaction.deleted_v1"} {
		resp := handler.Handle(context.Background(), feishuEventReq(t, eventType, map[string]any{
			"message_id":    "om_reacted",
			"reaction_type": map[string]any{"emoji_type": "DONE"},
			"operator_type": "user",
			"user_id":       map[string]any{"open_id": "ou_user"},
			"action_time":   "1710000000123",
		}))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("reaction %s status = %d body=%s", eventType, resp.StatusCode, string(resp.Body))
		}
	}

	resp := handler.Handle(context.Background(), feishuEventReq(t, "im.message.unknown_v1", map[string]any{"message_id": "om_unknown"}))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unknown event status = %d body=%s", resp.StatusCode, string(resp.Body))
	}
	if messageCalls != 0 {
		t.Fatalf("message handler called for non-message events: %d", messageCalls)
	}
	if reactionCalls != 2 {
		t.Fatalf("reaction handler calls = %d, want 2", reactionCalls)
	}
}

func TestFeishuEventDispatcherRejectsMalformedJSON(t *testing.T) {
	called := false
	handler := dispatcher.NewEventDispatcher("", "").
		OnP2MessageReceiveV1(func(_ context.Context, event *larkim.P2MessageReceiveV1) error {
			called = true
			return nil
		})

	resp := handler.Handle(context.Background(), &larkevent.EventReq{
		Header: http.Header{},
		Body:   []byte(`{"schema":"2.0","header":`),
	})
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("malformed event status = %d body=%s", resp.StatusCode, string(resp.Body))
	}
	if called {
		t.Fatal("message handler called for malformed JSON")
	}
}

func TestDecideFeishuMentionCoversGroupFiltering(t *testing.T) {
	botMention := []*larkim.MentionEvent{{
		Key: stringPtr("@_user_1"),
		Id:  &larkim.UserId{OpenId: stringPtr("ou_bot")},
	}}
	base := feishuInboundMessage{
		chatType: "group",
		msgType:  "text",
		content:  `{"text":"hello"}`,
	}

	if got := decideFeishuMention(base, "ou_bot", false, false, false); got.allow {
		t.Fatal("group message without bot mention should be blocked")
	}
	withMention := base
	withMention.mentions = botMention
	if got := decideFeishuMention(withMention, "ou_bot", false, false, false); !got.allow {
		t.Fatal("bot mention should pass group filter")
	}
	if got := decideFeishuMention(base, "ou_bot", true, false, false); !got.allow {
		t.Fatal("group_reply_all should bypass mention requirement")
	}
	atAll := base
	atAll.content = `{"text":"@_all hello"}`
	got := decideFeishuMention(atAll, "ou_bot", false, true, false)
	if !got.allow || !got.atEveryone {
		t.Fatalf("@all decision = %#v, want allow atEveryone", got)
	}
}

func TestDecideFeishuMentionRoutesActiveThreadAttachmentsOnly(t *testing.T) {
	in := feishuInboundMessage{chatType: "group", msgType: "image"}
	got := decideFeishuMention(in, "ou_bot", false, false, true)
	if !got.allow || !got.activeThreadAttachment {
		t.Fatalf("active thread image decision = %#v, want attachment pass-through", got)
	}

	in.msgType = "file"
	got = decideFeishuMention(in, "ou_bot", false, false, true)
	if !got.allow || !got.activeThreadAttachment {
		t.Fatalf("active thread file decision = %#v, want attachment pass-through", got)
	}

	in.msgType = "text"
	got = decideFeishuMention(in, "ou_bot", false, false, true)
	if got.allow {
		t.Fatalf("active thread text without mention should not pass, got %#v", got)
	}
}

func feishuEventReq(t *testing.T, eventType string, eventBody map[string]any) *larkevent.EventReq {
	t.Helper()

	body, err := json.Marshal(map[string]any{
		"schema": "2.0",
		"header": map[string]any{
			"event_id":    "ev_" + eventType,
			"event_type":  eventType,
			"app_id":      "cli_test",
			"tenant_key":  "tenant_test",
			"create_time": "1710000000123",
		},
		"event": eventBody,
	})
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	return &larkevent.EventReq{
		Header:     http.Header{},
		Body:       body,
		RequestURI: "/webhook/event",
	}
}
