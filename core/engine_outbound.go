package core

import (
	"log/slog"
	"strings"
	"time"
)

// waitOutgoing blocks on the per-platform outgoing rate limiter when enabled.
func (e *Engine) waitOutgoing(p Platform) error {
	if e.outgoingRL == nil {
		return nil
	}
	return e.outgoingRL.Wait(e.ctx, p.Name())
}

func (e *Engine) renderOutgoingContentForWorkspace(p Platform, content, workspaceDir string) string {
	if strings.TrimSpace(content) == "" {
		return content
	}
	return TransformLocalReferences(content, e.references, e.agent.Name(), p.Name(), workspaceDir)
}

func (e *Engine) sendWithErrorForWorkspace(p Platform, replyCtx any, content, workspaceDir string) error {
	if err := e.waitOutgoing(p); err != nil {
		slog.Warn("outgoing rate limit: context cancelled", "platform", p.Name(), "error", err)
		return err
	}
	content = e.renderOutgoingContentForWorkspace(p, content, workspaceDir)
	return e.sendAlreadyRenderedWithError(p, replyCtx, content)
}

func (e *Engine) sendForWorkspace(p Platform, replyCtx any, content, workspaceDir string) {
	_ = e.sendWithErrorForWorkspace(p, replyCtx, content, workspaceDir)
}

func (e *Engine) renderCardForPlatform(p Platform, card *Card) *Card {
	return e.renderCardForPlatformWorkspace(p, card, "")
}

func (e *Engine) renderCardForPlatformWorkspace(p Platform, card *Card, workspaceDir string) *Card {
	if card == nil {
		return nil
	}
	out := &Card{}
	if card.Header != nil {
		h := *card.Header
		out.Header = &h
	}
	out.Elements = make([]CardElement, 0, len(card.Elements))
	for _, elem := range card.Elements {
		switch v := elem.(type) {
		case CardMarkdown:
			content := v.Content
			if workspaceDir != "" {
				content = e.renderOutgoingContentForWorkspace(p, v.Content, workspaceDir)
			}
			out.Elements = append(out.Elements, CardMarkdown{Content: content})
		case CardNote:
			text := v.Text
			if workspaceDir != "" {
				text = e.renderOutgoingContentForWorkspace(p, v.Text, workspaceDir)
			}
			out.Elements = append(out.Elements, CardNote{Text: text, Tag: v.Tag})
		case CardListItem:
			text := v.Text
			if workspaceDir != "" {
				text = e.renderOutgoingContentForWorkspace(p, v.Text, workspaceDir)
			}
			out.Elements = append(out.Elements, CardListItem{
				Text:     text,
				BtnText:  v.BtnText,
				BtnType:  v.BtnType,
				BtnValue: v.BtnValue,
				Extra:    v.Extra,
			})
		default:
			out.Elements = append(out.Elements, elem)
		}
	}
	return out
}

// sendWithError applies outgoing rate limiting and p.Send. It logs wait
// cancellation and platform failures, and returns a non-nil error on either.
func (e *Engine) sendWithError(p Platform, replyCtx any, content string) error {
	if err := e.waitOutgoing(p); err != nil {
		slog.Warn("outgoing rate limit: context cancelled", "platform", p.Name(), "error", err)
		return err
	}
	return e.sendAlreadyRenderedWithError(p, replyCtx, content)
}

func (e *Engine) sendAlreadyRenderedWithError(p Platform, replyCtx any, content string) error {
	start := time.Now()
	if err := p.Send(e.ctx, replyCtx, content); err != nil {
		// Check for context_token missing error
		if strings.Contains(err.Error(), "missing context_token") {
			slog.Error("platform send failed: context_token missing",
				"platform", p.Name(),
				"error", err,
				"content_len", len(content),
				"hint", "user needs to send a new message to refresh context_token")
		} else {
			slog.Error("platform send failed", "platform", p.Name(), "error", err, "content_len", len(content))
		}
		return err
	}
	if elapsed := time.Since(start); elapsed >= slowPlatformSend {
		slog.Warn("slow platform send", "platform", p.Name(), "elapsed", elapsed, "content_len", len(content))
	}
	return nil
}

// send wraps p.Send with error logging, slow-operation warnings, and outgoing rate limiting.
func (e *Engine) send(p Platform, replyCtx any, content string) {
	_ = e.sendWithError(p, replyCtx, content)
}

// sendRaw sends content without local-reference rendering. This is used for raw
// tool outputs, where preserving the original text is preferable to applying the
// agent-facing reference display transform.
func (e *Engine) sendRaw(p Platform, replyCtx any, content string) {
	if err := e.waitOutgoing(p); err != nil {
		slog.Warn("outgoing rate limit: context cancelled", "platform", p.Name(), "error", err)
		return
	}
	_ = e.sendAlreadyRenderedWithError(p, replyCtx, content)
}

// replyWithError applies outgoing rate limiting and p.Reply.
func (e *Engine) replyWithError(p Platform, replyCtx any, content string) error {
	if err := e.waitOutgoing(p); err != nil {
		slog.Warn("outgoing rate limit: context cancelled", "platform", p.Name(), "error", err)
		return err
	}
	start := time.Now()
	if err := p.Reply(e.ctx, replyCtx, content); err != nil {
		slog.Error("platform reply failed", "platform", p.Name(), "error", err, "content_len", len(content))
		return err
	}
	if elapsed := time.Since(start); elapsed >= slowPlatformSend {
		slog.Warn("slow platform reply", "platform", p.Name(), "elapsed", elapsed, "content_len", len(content))
	}
	return nil
}

// reply wraps p.Reply with error logging, slow-operation warnings, and outgoing rate limiting.
func (e *Engine) reply(p Platform, replyCtx any, content string) {
	_ = e.replyWithError(p, replyCtx, content)
}

// replyWithButtons sends a reply with inline buttons if the platform supports it,
// otherwise falls back to plain text reply.
func (e *Engine) replyWithButtons(p Platform, replyCtx any, content string, buttons [][]ButtonOption) {
	if err := e.waitOutgoing(p); err != nil {
		slog.Warn("outgoing rate limit: context cancelled", "platform", p.Name(), "error", err)
		return
	}
	if bs, ok := p.(InlineButtonSender); ok {
		if err := bs.SendWithButtons(e.ctx, replyCtx, content, buttons); err == nil {
			return
		}
	}
	e.reply(p, replyCtx, content)
}

func supportsCards(p Platform) bool {
	_, ok := p.(CardSender)
	return ok
}

// replyWithCard sends a structured card via CardSender.
// For platforms without card support, renders as plain text (no intermediate fallback).
func (e *Engine) replyWithCard(p Platform, replyCtx any, card *Card) {
	if card == nil {
		slog.Error("replyWithCard: nil card", "platform", p.Name())
		return
	}
	if err := e.waitOutgoing(p); err != nil {
		slog.Warn("outgoing rate limit: context cancelled", "platform", p.Name(), "error", err)
		return
	}
	if cs, ok := p.(CardSender); ok {
		rendered := e.renderCardForPlatform(p, card)
		if err := cs.ReplyCard(e.ctx, replyCtx, rendered); err != nil {
			slog.Error("card reply failed", "platform", p.Name(), "error", err)
		}
		return
	}
	e.reply(p, replyCtx, e.renderCardForPlatform(p, card).RenderText())
}

// sendWithCard sends a card as a new message (not a reply).
func (e *Engine) sendWithCard(p Platform, replyCtx any, card *Card) {
	if card == nil {
		slog.Error("sendWithCard: nil card", "platform", p.Name())
		return
	}
	if err := e.waitOutgoing(p); err != nil {
		slog.Warn("outgoing rate limit: context cancelled", "platform", p.Name(), "error", err)
		return
	}
	if cs, ok := p.(CardSender); ok {
		rendered := e.renderCardForPlatform(p, card)
		if err := cs.SendCard(e.ctx, replyCtx, rendered); err != nil {
			slog.Error("card send failed", "platform", p.Name(), "error", err)
		}
		return
	}
	e.send(p, replyCtx, e.renderCardForPlatform(p, card).RenderText())
}
