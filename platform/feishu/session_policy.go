package feishu

import (
	"fmt"
	"strings"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

type feishuSessionPolicy struct {
	platformName          string
	shareSessionInChannel bool
	threadIsolation       bool
	noReplyToTrigger      bool
}

func (p *Platform) sessionPolicy() feishuSessionPolicy {
	return feishuSessionPolicy{
		platformName:          p.tag(),
		shareSessionInChannel: p.shareSessionInChannel,
		threadIsolation:       p.threadIsolation,
		noReplyToTrigger:      p.noReplyToTrigger,
	}
}

func (p *Platform) makeSessionKey(msg *larkim.EventMessage, chatID, userID string) string {
	return p.sessionPolicy().sessionKeyForMessage(msg, chatID, userID)
}

func (p *Platform) sessionKeyFromCardAction(chatID, userID string, value map[string]any) string {
	return p.sessionPolicy().sessionKeyFromCardAction(chatID, userID, value)
}

func (p *Platform) shouldReplyInThread(rc replyContext) bool {
	return p.sessionPolicy().shouldReplyInThread(rc)
}

// shouldUseThreadOrReplyAPI is true when we should call Im.Message.Reply
// (optionally with ReplyInThread).
func (p *Platform) shouldUseThreadOrReplyAPI(rc replyContext) bool {
	return p.sessionPolicy().shouldUseThreadOrReplyAPI(rc)
}

func (p *Platform) ReconstructReplyCtx(sessionKey string) (any, error) {
	return p.sessionPolicy().reconstructReplyCtx(sessionKey)
}

// RelayGroupVisibilityKey implements core.RelayGroupVisibilityTarget for
// feishu. When the caller session key targets a feishu thread, the visibility
// echo gets routed back into that thread; otherwise core falls back to the
// channel-level ":relay" default.
func (p *Platform) RelayGroupVisibilityKey(callerSessionKey string) (string, bool) {
	return relayGroupVisibilityKey(callerSessionKey)
}

func (policy feishuSessionPolicy) sessionKeyForMessage(msg *larkim.EventMessage, chatID, userID string) string {
	if policy.threadIsolation && msg != nil && stringValue(msg.ChatType) == "group" {
		rootID := stringValue(msg.RootId)
		if rootID == "" {
			rootID = stringValue(msg.MessageId)
		}
		if rootID != "" {
			return fmt.Sprintf("%s:%s:root:%s", policy.platformName, chatID, rootID)
		}
	}
	return policy.channelSessionKey(chatID, userID)
}

func (policy feishuSessionPolicy) sessionKeyFromCardAction(chatID, userID string, value map[string]any) string {
	if value != nil {
		if sessionKey, _ := value["session_key"].(string); sessionKey != "" {
			return sessionKey
		}
	}
	return policy.channelSessionKey(chatID, userID)
}

func (policy feishuSessionPolicy) channelSessionKey(chatID, userID string) string {
	if policy.shareSessionInChannel {
		return fmt.Sprintf("%s:%s", policy.platformName, chatID)
	}
	return fmt.Sprintf("%s:%s:%s", policy.platformName, chatID, userID)
}

func (policy feishuSessionPolicy) shouldReplyInThread(rc replyContext) bool {
	if rc.messageID == "" {
		return false
	}
	return policy.threadIsolation && isThreadSessionKey(rc.sessionKey)
}

func (policy feishuSessionPolicy) shouldUseThreadOrReplyAPI(rc replyContext) bool {
	if rc.messageID == "" {
		return false
	}
	return !policy.noReplyToTrigger
}

func (policy feishuSessionPolicy) reconstructReplyCtx(sessionKey string) (replyContext, error) {
	parts := strings.SplitN(sessionKey, ":", 3)
	if len(parts) < 2 || parts[0] != policy.platformName {
		return replyContext{}, fmt.Errorf("%s: invalid session key %q", policy.platformName, sessionKey)
	}
	rc := replyContext{chatID: parts[1], sessionKey: sessionKey}
	if len(parts) == 3 {
		if rootID, ok := parseThreadRootID(parts[2]); ok {
			rc.messageID = rootID
		}
	}
	return rc, nil
}

func relayGroupVisibilityKey(callerSessionKey string) (string, bool) {
	parts := strings.SplitN(callerSessionKey, ":", 3)
	if len(parts) < 3 || parts[0] != "feishu" {
		return "", false
	}
	chatID := parts[1]
	third := parts[2]
	for _, pfx := range []string{"root:", "thread:"} {
		if after, ok := strings.CutPrefix(third, pfx); ok && after != "" {
			return "feishu:" + chatID + ":" + third, true
		}
	}
	return "", false
}

func parseThreadRootID(sessionTail string) (string, bool) {
	for _, prefix := range []string{"root:", "thread:"} {
		if strings.HasPrefix(sessionTail, prefix) {
			rootID := strings.TrimPrefix(sessionTail, prefix)
			if rootID != "" {
				return rootID, true
			}
			return "", false
		}
	}
	return "", false
}

func isThreadSessionKey(sessionKey string) bool {
	parts := strings.SplitN(sessionKey, ":", 3)
	if len(parts) != 3 {
		return false
	}
	_, ok := parseThreadRootID(parts[2])
	return ok
}
