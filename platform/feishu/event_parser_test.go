package feishu

import (
	"testing"

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
	if dispatch.content != content || dispatch.messageID != "om_msg" || dispatch.userID != "ou_user" {
		t.Fatalf("dispatch input lost content/message/user: %#v", dispatch)
	}
	if dispatch.rctx != (replyContext{messageID: "om_msg", chatID: "oc_chat", sessionKey: "feishu:oc_chat:ou_user"}) {
		t.Fatalf("reply context = %#v", dispatch.rctx)
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
