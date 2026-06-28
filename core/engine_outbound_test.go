package core

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type outboundReconstructPlatform struct {
	stubPlatformEngine
	reconstructed []string
	replyContexts []any
	sendErr       error
}

func (p *outboundReconstructPlatform) ReconstructReplyCtx(sessionKey string) (any, error) {
	p.reconstructed = append(p.reconstructed, sessionKey)
	return "reply:" + sessionKey, nil
}

func (p *outboundReconstructPlatform) Send(_ context.Context, replyCtx any, content string) error {
	if p.sendErr != nil {
		return p.sendErr
	}
	p.mu.Lock()
	p.replyContexts = append(p.replyContexts, replyCtx)
	p.sent = append(p.sent, content)
	p.mu.Unlock()
	return nil
}

func (p *outboundReconstructPlatform) sentReplyContexts() []any {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]any, len(p.replyContexts))
	copy(out, p.replyContexts)
	return out
}

func TestSendWithCard_FallsBackToTextWhenPlatformHasNoCardSupport(t *testing.T) {
	p := &stubPlatformEngine{n: "plain"}
	e := NewEngine("test", &stubAgent{}, []Platform{p}, "", LangEnglish)
	card := NewCard().Title("Help", "blue").Markdown("Plain fallback").Build()

	e.sendWithCard(p, "ctx", card)

	if len(p.sent) != 1 {
		t.Fatalf("sent messages = %d, want 1", len(p.sent))
	}
	if got, want := p.sent[0], card.RenderText(); got != want {
		t.Fatalf("fallback text = %q, want %q", got, want)
	}
}

func TestSendWithCard_UsesCardSenderWhenSupported(t *testing.T) {
	p := &stubCardPlatform{stubPlatformEngine: stubPlatformEngine{n: "card"}}
	e := NewEngine("test", &stubAgent{}, []Platform{p}, "", LangEnglish)
	card := NewCard().Markdown("Interactive").Build()

	e.sendWithCard(p, "ctx", card)

	if len(p.sentCards) != 1 {
		t.Fatalf("sent cards = %d, want 1", len(p.sentCards))
	}
	if len(p.sent) != 0 {
		t.Fatalf("plain sends = %d, want 0", len(p.sent))
	}
}

func TestQueueMessageForBusySession_StoresMetadataAndReportsFull(t *testing.T) {
	p := &stubPlatformEngine{n: "feishu"}
	e := NewEngine("test", &stubAgent{}, []Platform{p}, "", LangEnglish)
	e.SetMaxQueuedMessages(1)
	sessionKey := "feishu:chat-1:user-1"
	state := &interactiveState{
		agentSession: newControllableSession("busy"),
		platform:     p,
		replyCtx:     "active-reply",
	}

	e.interactiveMu.Lock()
	e.interactiveStates[sessionKey] = state
	e.interactiveMu.Unlock()

	first := &Message{
		SessionKey:        sessionKey,
		Platform:          "feishu",
		MessageID:         "m1",
		UserID:            "u1",
		UserName:          "Alice",
		Content:           "queued text",
		Images:            []ImageAttachment{{MimeType: "image/png", Data: []byte("img"), FileName: "a.png"}},
		Files:             []FileAttachment{{MimeType: "text/plain", Data: []byte("file"), FileName: "a.txt"}},
		ChannelKey:        "chat-1:thread-1",
		ReplyCtx:          "reply-1",
		UserMessageTimeMs: 100,
	}
	if !e.queueMessageForBusySession(p, first, sessionKey) {
		t.Fatal("queueMessageForBusySession() = false, want handled")
	}

	state.mu.Lock()
	if len(state.pendingMessages) != 1 {
		t.Fatalf("pending messages = %d, want 1", len(state.pendingMessages))
	}
	queued := state.pendingMessages[0]
	state.mu.Unlock()
	if queued.messageID != "m1" || queued.replyCtx != "reply-1" || queued.content != "queued text" {
		t.Fatalf("queued metadata = %#v, want original message id/reply/content", queued)
	}
	if queued.userID != "u1" || queued.userName != "Alice" || queued.msgPlatform != "feishu" || queued.msgSessionKey != sessionKey {
		t.Fatalf("queued sender metadata = %#v, want original sender fields", queued)
	}
	if queued.channelKey != "chat-1:thread-1" || queued.userMessageTimeMs != 100 {
		t.Fatalf("queued channel/time = %q/%d, want original values", queued.channelKey, queued.userMessageTimeMs)
	}
	if len(queued.images) != 1 || len(queued.files) != 1 {
		t.Fatalf("queued attachments = %d images/%d files, want 1/1", len(queued.images), len(queued.files))
	}

	second := &Message{SessionKey: sessionKey, MessageID: "m2", Content: "overflow", ReplyCtx: "reply-2"}
	if !e.queueMessageForBusySession(p, second, sessionKey) {
		t.Fatal("queueMessageForBusySession() overflow = false, want handled")
	}
	state.mu.Lock()
	pending := len(state.pendingMessages)
	state.mu.Unlock()
	if pending != 1 {
		t.Fatalf("pending messages after overflow = %d, want still 1", pending)
	}
	sent := p.getSent()
	if len(sent) != 2 {
		t.Fatalf("queue replies = %#v, want queued notice and full notice", sent)
	}
	if !strings.Contains(sent[0], "Message received") {
		t.Fatalf("first reply = %q, want queued notice", sent[0])
	}
	if !strings.Contains(sent[1], "queue is full (1 pending)") {
		t.Fatalf("second reply = %q, want full queue notice", sent[1])
	}
}

func TestSendWithError_ReturnsRateLimitCancellationWithoutSending(t *testing.T) {
	p := &stubPlatformEngine{n: "feishu"}
	e := NewEngine("test", &stubAgent{}, []Platform{p}, "", LangEnglish)
	e.SetOutgoingRateLimitCfg(OutgoingRateLimitCfg{MaxPerSecond: 1, Burst: 1}, nil)

	if err := e.sendWithError(p, "reply", "first"); err != nil {
		t.Fatalf("first sendWithError() error = %v", err)
	}
	e.cancel()
	err := e.sendWithError(p, "reply", "second")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("second sendWithError() error = %v, want context canceled", err)
	}
	if got := p.getSent(); len(got) != 1 || got[0] != "first" {
		t.Fatalf("sent messages after cancellation = %#v, want only first", got)
	}
}

func TestSendToSession_ReconstructsThreadReplyContext(t *testing.T) {
	p := &outboundReconstructPlatform{stubPlatformEngine: stubPlatformEngine{n: "feishu"}}
	e := NewEngine("test", &stubAgent{}, []Platform{p}, "", LangEnglish)

	sessionKey := "feishu:chat-1:thread-9"
	if err := e.SendToSessionWithOptions(sessionKey, "thread reply", nil, nil, SendOptions{}); err != nil {
		t.Fatalf("SendToSessionWithOptions() error = %v", err)
	}

	if len(p.reconstructed) != 1 || p.reconstructed[0] != sessionKey {
		t.Fatalf("reconstructed keys = %#v, want [%q]", p.reconstructed, sessionKey)
	}
	if got := p.sentReplyContexts(); len(got) != 1 || got[0] != "reply:"+sessionKey {
		t.Fatalf("reply contexts = %#v, want reconstructed reply context", got)
	}
	if got := p.getSent(); len(got) != 1 || got[0] != "thread reply" {
		t.Fatalf("sent messages = %#v, want thread reply", got)
	}
}

func TestSendToSession_SendFailureDoesNotMarkSideTextAndCanRecover(t *testing.T) {
	sendErr := errors.New("platform send failed")
	p := &outboundReconstructPlatform{
		stubPlatformEngine: stubPlatformEngine{n: "feishu"},
		sendErr:            sendErr,
	}
	e := NewEngine("test", &stubAgent{}, []Platform{p}, "", LangEnglish)
	sessionKey := "feishu:chat-1:user-1"
	state := &interactiveState{platform: p, replyCtx: "reply-1"}
	e.interactiveMu.Lock()
	e.interactiveStates[sessionKey] = state
	e.interactiveMu.Unlock()

	err := e.SendToSessionWithOptions(sessionKey, "first", nil, nil, SendOptions{})
	if !errors.Is(err, sendErr) {
		t.Fatalf("SendToSessionWithOptions() error = %v, want send error", err)
	}
	state.mu.Lock()
	sideText := state.sideText
	state.mu.Unlock()
	if sideText != "" {
		t.Fatalf("sideText after failed send = %q, want empty", sideText)
	}

	p.sendErr = nil
	if err := e.SendToSessionWithOptions(sessionKey, "second", nil, nil, SendOptions{}); err != nil {
		t.Fatalf("recovery SendToSessionWithOptions() error = %v", err)
	}
	state.mu.Lock()
	sideText = state.sideText
	state.mu.Unlock()
	if sideText != "second" {
		t.Fatalf("sideText after recovery = %q, want second", sideText)
	}
	if got := p.getSent(); len(got) != 1 || got[0] != "second" {
		t.Fatalf("sent messages after recovery = %#v, want successful second send", got)
	}
}
