package feishu

import (
	"strconv"
	"strings"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

type feishuInboundMessage struct {
	msgType      string
	chatID       string
	chatType     string
	userID       string
	messageID    string
	content      string
	hasContent   bool
	mentions     []*larkim.MentionEvent
	parentID     string
	rootID       string
	threadID     string
	createTime   string
	createTimeMs int64
	message      *larkim.EventMessage
}

func parseFeishuInboundMessage(event *larkim.P2MessageReceiveV1) feishuInboundMessage {
	if event == nil || event.Event == nil {
		return feishuInboundMessage{}
	}

	msg := event.Event.Message
	sender := event.Event.Sender
	in := feishuInboundMessage{
		userID: userIDFromEvent(nil),
	}
	if sender != nil {
		in.userID = userIDFromEvent(sender.SenderId)
	}
	if msg == nil {
		return in
	}

	in.message = msg
	in.msgType = stringValue(msg.MessageType)
	in.chatID = stringValue(msg.ChatId)
	in.chatType = stringValue(msg.ChatType)
	in.messageID = stringValue(msg.MessageId)
	in.parentID = stringValue(msg.ParentId)
	in.rootID = stringValue(msg.RootId)
	in.threadID = stringValue(msg.ThreadId)
	in.createTime = stringValue(msg.CreateTime)
	in.mentions = msg.Mentions
	if msg.Content != nil {
		in.content = *msg.Content
		in.hasContent = true
	}
	if in.createTime != "" {
		if ms, err := strconv.ParseInt(in.createTime, 10, 64); err == nil {
			in.createTimeMs = ms
		}
	}
	return in
}

type feishuMentionDecision struct {
	allow                  bool
	atEveryone             bool
	activeThreadAttachment bool
}

func decideFeishuMention(in feishuInboundMessage, botOpenID string, groupReplyAll, respondToAtEveryoneAndHere, activeThreadSession bool) feishuMentionDecision {
	if in.chatType != "group" || groupReplyAll || botOpenID == "" {
		return feishuMentionDecision{allow: true}
	}
	if isBotMentioned(in.mentions, botOpenID) {
		return feishuMentionDecision{allow: true}
	}
	if respondToAtEveryoneAndHere && strings.Contains(in.content, "@_all") {
		return feishuMentionDecision{allow: true, atEveryone: true}
	}
	if activeThreadSession && isAttachmentMsgType(in.msgType) {
		return feishuMentionDecision{allow: true, activeThreadAttachment: true}
	}
	return feishuMentionDecision{}
}

type feishuDispatchInput struct {
	msgType      string
	content      string
	mentions     []*larkim.MentionEvent
	messageID    string
	sessionKey   string
	userID       string
	chatID       string
	rctx         replyContext
	parentID     string
	createTimeMs int64
}

func (in feishuInboundMessage) dispatchInput(sessionKey string) feishuDispatchInput {
	return feishuDispatchInput{
		msgType:      in.msgType,
		content:      in.content,
		mentions:     in.mentions,
		messageID:    in.messageID,
		sessionKey:   sessionKey,
		userID:       in.userID,
		chatID:       in.chatID,
		rctx:         replyContext{messageID: in.messageID, chatID: in.chatID, sessionKey: sessionKey},
		parentID:     in.parentID,
		createTimeMs: in.createTimeMs,
	}
}

func isBotMentioned(mentions []*larkim.MentionEvent, botOpenID string) bool {
	for _, m := range mentions {
		if m.Id != nil && m.Id.OpenId != nil && *m.Id.OpenId == botOpenID {
			return true
		}
	}
	return false
}

// isAttachmentMsgType reports whether a Feishu message type carries only an
// attachment payload (no free-form text the user could use to address another
// human). These are the message types we are willing to admit into an
// already-engaged thread without an explicit @bot mention.
func isAttachmentMsgType(msgType string) bool {
	switch msgType {
	case "image", "file", "audio", "media":
		return true
	}
	return false
}

// stripMentions processes @mention placeholders (e.g. @_user_1) in text.
// The bot's own mention is removed; other user mentions are replaced with
// their display name so the agent can see who was referenced.
func stripMentions(text string, mentions []*larkim.MentionEvent, botOpenID string) string {
	if len(mentions) == 0 {
		return text
	}
	for _, m := range mentions {
		if m.Key == nil {
			continue
		}
		if botOpenID != "" && m.Id != nil && m.Id.OpenId != nil && *m.Id.OpenId == botOpenID {
			text = strings.ReplaceAll(text, *m.Key, "")
		} else if m.Name != nil && *m.Name != "" {
			text = strings.ReplaceAll(text, *m.Key, "@"+*m.Name)
		} else {
			text = strings.ReplaceAll(text, *m.Key, "")
		}
	}
	return strings.TrimSpace(text)
}
