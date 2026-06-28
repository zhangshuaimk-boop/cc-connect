package feishu

import (
	"log/slog"
	"time"

	"github.com/chenhg5/cc-connect/core"
)

// defaultImageBatchWindow is the quiet period after the last image in a
// session before the buffered batch is dispatched as a single multi-image
// message. 500ms covers real-world mobile sending intervals while remaining
// responsive for sequential single image sends.
const defaultImageBatchWindow = 500 * time.Millisecond

// imageBatchEntry holds image data accumulated for one session while we wait
// to see if more images are coming.
type imageBatchEntry struct {
	sessionKey   string
	userID       string
	userName     string
	chatName     string
	rctx         replyContext
	quoted       quotedMessage
	images       []core.ImageAttachment
	messageIDs   []string
	createTimeMs int64
	parentID     string
	timer        *time.Timer
}

// batchWindow returns the effective image-batch coalesce window for this
// Platform. Tests and zero-initialised Platforms fall back to the default so
// they never schedule a zero-duration timer.
func (p *Platform) batchWindow() time.Duration {
	if p.imageBatchWindow > 0 {
		return p.imageBatchWindow
	}
	return defaultImageBatchWindow
}

// bufferImage adds a freshly-downloaded image to the per-session batch buffer.
// Consecutive image-only messages from the same session coalesce into a single
// multi-image dispatch after imageBatchWindow of quiet time.
func (p *Platform) bufferImage(sessionKey string, entry *imageBatchEntry) {
	p.imageBatchMu.Lock()
	if p.imageBatch == nil {
		p.imageBatch = make(map[string]*imageBatchEntry)
	}

	var toFlush *imageBatchEntry
	if existing, ok := p.imageBatch[sessionKey]; ok {
		if existing.parentID != entry.parentID || existing.userID != entry.userID {
			if existing.timer != nil {
				existing.timer.Stop()
			}
			delete(p.imageBatch, sessionKey)
			toFlush = existing
		}
	}

	if existing, ok := p.imageBatch[sessionKey]; ok {
		if existing.timer != nil {
			existing.timer.Stop()
		}
		existing.images = append(existing.images, entry.images...)
		existing.messageIDs = append(existing.messageIDs, entry.messageIDs...)
		if entry.createTimeMs > existing.createTimeMs {
			existing.createTimeMs = entry.createTimeMs
		}
		ref := existing
		existing.timer = time.AfterFunc(p.batchWindow(), func() {
			p.flushImageBatchByRef(sessionKey, ref)
		})
	} else {
		ref := entry
		entry.timer = time.AfterFunc(p.batchWindow(), func() {
			p.flushImageBatchByRef(sessionKey, ref)
		})
		p.imageBatch[sessionKey] = entry
	}

	p.imageBatchMu.Unlock()

	if toFlush != nil {
		p.dispatchImageBatchEntry(toFlush)
	}
}

// flushImageBatchByRef dispatches the batch iff the map still points at the
// same entry pointer. Called by the AfterFunc timer callback.
func (p *Platform) flushImageBatchByRef(sessionKey string, ref *imageBatchEntry) {
	p.imageBatchMu.Lock()
	current, ok := p.imageBatch[sessionKey]
	if !ok || current != ref {
		p.imageBatchMu.Unlock()
		return
	}
	if current.timer != nil {
		current.timer.Stop()
	}
	delete(p.imageBatch, sessionKey)
	p.imageBatchMu.Unlock()

	p.dispatchImageBatchEntry(current)
}

// flushImageBatches synchronously dispatches any pending image batches.
func (p *Platform) flushImageBatches() {
	p.imageBatchMu.Lock()
	pending := p.imageBatch
	p.imageBatch = make(map[string]*imageBatchEntry)
	p.imageBatchMu.Unlock()

	for _, entry := range pending {
		if entry.timer != nil {
			entry.timer.Stop()
		}
		p.dispatchImageBatchEntry(entry)
	}
}

// dispatchImageBatchEntry emits a single core.Message carrying all images
// buffered into this batch entry.
func (p *Platform) dispatchImageBatchEntry(entry *imageBatchEntry) {
	if len(entry.images) == 0 {
		return
	}
	lastIdx := len(entry.messageIDs) - 1
	canonicalID := entry.messageIDs[lastIdx]
	for _, mid := range entry.messageIDs {
		if p.isMessageRecalled(mid) {
			slog.Debug(p.tag()+": recalled image batch member dropped",
				"message_id", mid, "batch_size", len(entry.images))
		}
	}
	slog.Info(p.tag()+": dispatched image batch",
		"session_key", entry.sessionKey,
		"image_count", len(entry.images),
		"message_ids", entry.messageIDs,
	)
	p.dispatchCoreMessage(&core.Message{
		SessionKey: entry.sessionKey, Platform: p.platformName,
		MessageID: canonicalID,
		UserID:    entry.userID, UserName: entry.userName, ChatName: entry.chatName,
		Content:           "",
		ExtraContent:      entry.quoted.text,
		Images:            append(entry.quoted.images, entry.images...),
		ReplyCtx:          entry.rctx,
		UserMessageTimeMs: entry.createTimeMs,
	})
}
