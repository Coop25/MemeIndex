package manager

import (
	"context"
	"errors"
	"fmt"
	"log"
	"memeindex/internal/accessor"
	"memeindex/internal/tagsuggest"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var errAutoSuggestExhausted = errors.New("tag suggestion attempts exhausted")

type MemeTagSuggestionResult struct {
	Tags   []string `json:"tags"`
	Model  string   `json:"model"`
	Source string   `json:"source"`
}

type TagSuggestionQueueStatus struct {
	Enabled                    bool                      `json:"enabled"`
	OllamaReady                bool                      `json:"ollama_ready"`
	OllamaState                string                    `json:"ollama_state"`
	Model                      string                    `json:"model"`
	QueueLength                int                       `json:"queue_length"`
	Processing                 bool                      `json:"processing"`
	CurrentMemeID              string                    `json:"current_meme_id,omitempty"`
	CurrentMemeName            string                    `json:"current_meme_name,omitempty"`
	UntaggedWithoutSuggestions int                       `json:"untagged_without_suggestions"`
	PendingSuggestionMemes     int                       `json:"pending_suggestion_memes"`
	LastError                  string                    `json:"last_error,omitempty"`
	LastSuccessAt              time.Time                 `json:"last_success_at,omitempty"`
	QueuedMemes                []QueuedTagSuggestionItem `json:"queued_memes,omitempty"`
	PendingReviewMemes         []PendingReviewMemeItem   `json:"pending_review_memes,omitempty"`
	PendingReviewOffset        int                       `json:"pending_review_offset"`
	PendingReviewLimit         int                       `json:"pending_review_limit"`
	PendingReviewHasMore       bool                      `json:"pending_review_has_more"`
	PendingReviewNextOffset    int                       `json:"pending_review_next_offset"`
}

type QueuedTagSuggestionItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type PendingReviewMemeItem struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	SuggestedTags []string `json:"suggested_tags,omitempty"`
}

type ResetTagSuggestionQueueResult struct {
	ClearedSuggestions int `json:"cleared_suggestions"`
	ClearedExhausted   int `json:"cleared_exhausted"`
	QueuedUntagged     int `json:"queued_untagged"`
}

type tagSuggestionAttemptOptions struct {
	imageWidth        int
	videoFrameCount   int
	videoFrameWidth   int
	includeTranscript bool
	sourceSuffix      string
}

func (m *MemeManager) GetStoredMemeTagSuggestions(ctx context.Context, id string) (MemeTagSuggestionResult, error) {
	suggestionStore, ok := m.store.(accessor.SuggestedTagStore)
	if !ok {
		return MemeTagSuggestionResult{}, tagsuggest.ErrDisabled
	}

	tags, err := suggestionStore.ListSuggestedTags(ctx, strings.TrimSpace(id))
	if err != nil {
		return MemeTagSuggestionResult{}, err
	}
	return MemeTagSuggestionResult{
		Tags: append([]string(nil), tags...),
	}, nil
}

func (m *MemeManager) RefreshMemeTagSuggestions(ctx context.Context, userID string, id string) (MemeTagSuggestionResult, error) {
	if m.tagSuggester == nil || !m.tagSuggester.Enabled() {
		return MemeTagSuggestionResult{}, tagsuggest.ErrDisabled
	}

	meme, err := m.store.GetByID(ctx, strings.TrimSpace(userID), strings.TrimSpace(id))
	if err != nil {
		return MemeTagSuggestionResult{}, err
	}

	result, err := m.suggestTagsForMeme(ctx, meme)
	if err != nil {
		return MemeTagSuggestionResult{}, err
	}

	output := MemeTagSuggestionResult{
		Tags:   append([]string(nil), result.Tags...),
		Model:  result.Model,
		Source: result.Source,
	}

	if suggestionStore, ok := m.store.(accessor.SuggestedTagStore); ok {
		// The pass behind this result can take minutes of model time, and its
		// context (the request's, or the worker's per-meme deadline) may end
		// just as it finishes. Record what it worked out regardless: these
		// writes are quick, and dropping them only makes the next pass redo
		// the work, or leave a meme with its new suggestions but still marked
		// exhausted.
		ctx := context.WithoutCancel(ctx)
		if err := suggestionStore.ReplaceSuggestedTags(ctx, meme.ID, output.Tags); err != nil {
			return MemeTagSuggestionResult{}, err
		}
		if err := suggestionStore.SetAutoSuggestDisabled(ctx, meme.ID, false); err != nil {
			return MemeTagSuggestionResult{}, err
		}
		// Persist the derived search text (on-image text + transcript) so it
		// feeds meme search. Best-effort: a failure here must not fail the
		// suggestion.
		if err := suggestionStore.SetSearchText(ctx, meme.ID, result.Text); err != nil {
			log.Printf("tag suggestion: storing search text for meme %s failed: %v", meme.ID, err)
		}
	}

	return output, nil
}

func (m *MemeManager) QueueMemeTagSuggestions(ctx context.Context, id string) {
	if m.tagSuggester == nil || !m.tagSuggester.Enabled() {
		return
	}
	memeID := strings.TrimSpace(id)
	if memeID == "" {
		return
	}

	meme, err := m.store.GetByID(ctx, "", memeID)
	if err != nil {
		return
	}
	if !shouldQueueTagSuggestionsForMeme(meme) {
		return
	}

	m.enqueueTagSuggestion(memeID)
}

func (m *MemeManager) ResetTagSuggestionsAndRequeueUntagged(ctx context.Context, actor accessor.AuditActor) (ResetTagSuggestionQueueResult, error) {
	// A reset is one write per meme followed by the audit entry that records
	// it. Stopped part-way by a cancelled request it would leave the library
	// half reset with nothing in the audit log, so it runs to the end.
	ctx = context.WithoutCancel(ctx)
	suggestionStore, ok := m.store.(accessor.SuggestedTagStore)
	if !ok {
		return ResetTagSuggestionQueueResult{}, tagsuggest.ErrDisabled
	}

	memes := m.store.List(ctx, "", "", false, "")
	result := ResetTagSuggestionQueueResult{}
	for _, meme := range memes {
		if len(meme.SuggestedTags) > 0 {
			if err := suggestionStore.ReplaceSuggestedTags(ctx, meme.ID, nil); err != nil {
				return ResetTagSuggestionQueueResult{}, err
			}
			result.ClearedSuggestions += 1
			// The same pass that produced those suggestions also set the
			// derived search text; drop it so a re-run regenerates it cleanly.
			if err := suggestionStore.SetSearchText(ctx, meme.ID, ""); err != nil {
				return ResetTagSuggestionQueueResult{}, err
			}
		}
		if meme.AutoSuggestDisabled {
			if err := suggestionStore.SetAutoSuggestDisabled(ctx, meme.ID, false); err != nil {
				return ResetTagSuggestionQueueResult{}, err
			}
			result.ClearedExhausted += 1
		}

		if len(meme.Tags) == 0 && m.enqueueTagSuggestion(meme.ID) {
			result.QueuedUntagged += 1
		}
	}

	m.invalidateAdminReadCache()
	if writer, ok := m.store.(accessor.AuditStore); ok {
		description := fmt.Sprintf(
			"Reset tag suggestions: cleared %d suggestion set(s), re-enabled %d exhausted, requeued %d untagged",
			result.ClearedSuggestions, result.ClearedExhausted, result.QueuedUntagged,
		)
		if err := writer.RecordSystemAudit(ctx, "tag_suggestions_reset", actor, description); err != nil {
			log.Printf("record tag suggestion reset audit failed: %v", err)
		}
	}

	return result, nil
}

type RetryFailedTagSuggestionsResult struct {
	ReenabledExhausted int `json:"reenabled_exhausted"`
	Queued             int `json:"queued"`
}

// RetryFailedTagSuggestions gives memes the suggester gave up on another go:
// it clears their auto-suggest-disabled mark and queues every untagged meme
// that has no suggestions. Unlike the reset it leaves existing suggestions
// alone.
func (m *MemeManager) RetryFailedTagSuggestions(ctx context.Context, actor accessor.AuditActor) (RetryFailedTagSuggestionsResult, error) {
	// One write per exhausted meme and then the audit entry: like the reset,
	// it runs to the end once started.
	ctx = context.WithoutCancel(ctx)
	suggestionStore, ok := m.store.(accessor.SuggestedTagStore)
	if !ok || m.tagSuggester == nil || !m.tagSuggester.Enabled() {
		return RetryFailedTagSuggestionsResult{}, tagsuggest.ErrDisabled
	}

	result := RetryFailedTagSuggestionsResult{}
	for _, meme := range m.store.List(ctx, "", "", false, "") {
		if !meme.AutoSuggestDisabled {
			continue
		}
		if err := suggestionStore.SetAutoSuggestDisabled(ctx, meme.ID, false); err != nil {
			return RetryFailedTagSuggestionsResult{}, err
		}
		result.ReenabledExhausted++
	}
	result.Queued = m.SeedTagSuggestionQueue(ctx)

	m.invalidateAdminReadCache()
	if writer, ok := m.store.(accessor.AuditStore); ok {
		description := fmt.Sprintf(
			"Retried failed tag suggestions: re-enabled %d exhausted, queued %d untagged",
			result.ReenabledExhausted, result.Queued,
		)
		if err := writer.RecordSystemAudit(ctx, "tag_suggestions_retry_failed", actor, description); err != nil {
			log.Printf("record tag suggestion retry audit failed: %v", err)
		}
	}
	return result, nil
}

func (m *MemeManager) TagSuggestionQueueStatus(ctx context.Context, reviewOffset int, reviewLimit int) TagSuggestionQueueStatus {
	if reviewOffset < 0 {
		reviewOffset = 0
	}
	if reviewLimit <= 0 {
		reviewLimit = 50
	}
	if reviewLimit > 100 {
		reviewLimit = 100
	}

	status := TagSuggestionQueueStatus{
		Enabled:             m.tagSuggester != nil && m.tagSuggester.Enabled(),
		PendingReviewOffset: reviewOffset,
		PendingReviewLimit:  reviewLimit,
	}
	if m.tagSuggester != nil {
		status.Model = m.tagSuggester.Model()
	}

	m.suggestionQueueMu.Lock()
	status.OllamaReady = m.suggestionWorkerReady
	status.OllamaState = m.suggestionWorkerState
	status.QueueLength = len(m.suggestionQueue)
	status.CurrentMemeID = m.suggestionCurrentID
	status.Processing = status.CurrentMemeID != ""
	status.LastError = m.suggestionLastError
	status.LastSuccessAt = m.suggestionLastSuccess
	queuedIDs := append([]string(nil), m.suggestionQueue...)
	m.suggestionQueueMu.Unlock()

	// Resolve every display name this poll needs in a single batch instead of
	// one GetByID (a LEFT JOIN + GROUP BY) per queued meme.
	nameLookup := make([]string, 0, len(queuedIDs)+1)
	if status.CurrentMemeID != "" {
		nameLookup = append(nameLookup, status.CurrentMemeID)
	}
	nameLookup = append(nameLookup, queuedIDs...)
	names := m.memeNamesByIDs(ctx, nameLookup)

	if status.CurrentMemeID != "" {
		status.CurrentMemeName = names[status.CurrentMemeID]
	}

	if len(queuedIDs) > 0 {
		status.QueuedMemes = make([]QueuedTagSuggestionItem, 0, len(queuedIDs))
		for _, id := range queuedIDs {
			status.QueuedMemes = append(status.QueuedMemes, QueuedTagSuggestionItem{ID: id, Name: names[id]})
		}
	}

	if tq, ok := m.store.(accessor.TagAdminStore); ok {
		if filled, ok := m.fillTagSuggestionCountsFromSQL(ctx, tq, &status, reviewOffset, reviewLimit); ok {
			return filled
		}
	}

	m.fillTagSuggestionCountsFromScan(ctx, &status, reviewOffset, reviewLimit)
	return status
}

// fillTagSuggestionCountsFromSQL populates the untagged/pending counters and the
// pending-review page from the database. It reports false (leaving status
// untouched) if either query fails so the caller can fall back to a scan.
func (m *MemeManager) fillTagSuggestionCountsFromSQL(ctx context.Context, tq accessor.TagAdminStore, status *TagSuggestionQueueStatus, reviewOffset, reviewLimit int) (TagSuggestionQueueStatus, bool) {
	// A cancelled request reports true ("handled") so the caller does not
	// fall back to a full scan for a client that has gone away.
	untagged, err := tq.UntaggedWithoutSuggestionsCount(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return *status, true
		}
		log.Printf("tag suggestion status: untagged count SQL failed, falling back to scan: %v", err)
		return TagSuggestionQueueStatus{}, false
	}
	total, pending, err := tq.PendingSuggestionMemes(ctx, reviewOffset, reviewLimit)
	if err != nil {
		if ctx.Err() != nil {
			return *status, true
		}
		log.Printf("tag suggestion status: pending-review SQL failed, falling back to scan: %v", err)
		return TagSuggestionQueueStatus{}, false
	}

	status.UntaggedWithoutSuggestions = untagged
	status.PendingSuggestionMemes = total
	for _, meme := range pending {
		status.PendingReviewMemes = append(status.PendingReviewMemes, PendingReviewMemeItem{
			ID:            meme.ID,
			Name:          meme.OriginalName,
			SuggestedTags: append([]string(nil), meme.SuggestedTags...),
		})
	}
	status.PendingReviewNextOffset = min(total, reviewOffset+len(status.PendingReviewMemes))
	status.PendingReviewHasMore = status.PendingReviewNextOffset < total
	return *status, true
}

func (m *MemeManager) fillTagSuggestionCountsFromScan(ctx context.Context, status *TagSuggestionQueueStatus, reviewOffset, reviewLimit int) {
	for _, meme := range m.store.List(ctx, "", "", false, "") {
		if shouldQueueTagSuggestionsForMeme(meme) {
			status.UntaggedWithoutSuggestions += 1
		}
		if len(meme.SuggestedTags) > 0 {
			status.PendingSuggestionMemes += 1
			pendingIndex := status.PendingSuggestionMemes - 1
			if pendingIndex >= reviewOffset && len(status.PendingReviewMemes) < reviewLimit {
				status.PendingReviewMemes = append(status.PendingReviewMemes, PendingReviewMemeItem{
					ID:            meme.ID,
					Name:          meme.OriginalName,
					SuggestedTags: append([]string(nil), meme.SuggestedTags...),
				})
			}
		}
	}
	status.PendingReviewNextOffset = min(status.PendingSuggestionMemes, reviewOffset+len(status.PendingReviewMemes))
	status.PendingReviewHasMore = status.PendingReviewNextOffset < status.PendingSuggestionMemes
}

func (m *MemeManager) StartTagSuggestionWorker() {
	if m.tagSuggester == nil || !m.tagSuggester.Enabled() {
		return
	}

	m.suggestionWorkerStart.Do(func() {
		m.workerWG.Add(1)
		go func() {
			defer m.workerWG.Done()
			m.runTagSuggestionWorker()
		}()
	})
}

func (m *MemeManager) SeedTagSuggestionQueue(ctx context.Context) int {
	if m.tagSuggester == nil || !m.tagSuggester.Enabled() {
		return 0
	}

	var (
		ids      []string
		resolved bool
	)
	if tq, ok := m.store.(accessor.TagAdminStore); ok {
		if got, err := tq.UntaggedWithoutSuggestionIDs(ctx); err == nil {
			ids, resolved = got, true
		} else if ctx.Err() != nil {
			return 0
		} else {
			log.Printf("seed tag suggestion queue: SQL failed, falling back to scan: %v", err)
		}
	}
	if !resolved {
		for _, meme := range m.store.List(ctx, "", "", false, "") {
			if shouldQueueTagSuggestionsForMeme(meme) {
				ids = append(ids, meme.ID)
			}
		}
	}

	queued := 0
	for _, id := range ids {
		if m.enqueueTagSuggestion(id) {
			queued += 1
		}
	}
	return queued
}

// maxTagSuggestionFailures is how many times the worker retries one meme after
// a timeout or an Ollama error before leaving it for the next restart or an
// admin retry. Without a cap a single slow video cycles through the queue
// forever.
const maxTagSuggestionFailures = 3

// waitForOllama blocks until Ollama answers with the model pulled. It returns
// false when the workers are stopping.
func (m *MemeManager) waitForOllama() bool {
	announced := false
	for {
		if m.workerCtx.Err() != nil {
			return false
		}
		ctx, cancel := context.WithTimeout(m.workerCtx, 30*time.Second)
		err := m.tagSuggester.Ready(ctx)
		cancel()
		if err == nil {
			if announced {
				log.Printf("tag suggestion worker: Ollama is ready")
			}
			return true
		}
		m.setTagSuggestionWorkerState("waiting_for_ollama", false, "", err.Error())
		if !announced {
			log.Printf("tag suggestion worker: waiting for Ollama model %q: %v", m.tagSuggester.Model(), err)
			announced = true
		}
		if !m.waitOrStop(3 * time.Second) {
			return false
		}
	}
}

// retryOrDropTagSuggestion requeues a meme after a transient failure, or
// drops it once it has failed maxTagSuggestionFailures times. It reports
// whether the meme was requeued.
func (m *MemeManager) retryOrDropTagSuggestion(id string, err error) bool {
	m.suggestionQueueMu.Lock()
	m.suggestionFailures[id]++
	giveUp := m.suggestionFailures[id] >= maxTagSuggestionFailures
	m.suggestionQueueMu.Unlock()

	if giveUp {
		log.Printf("tag suggestion worker: giving up on meme %s after %d failed attempts: %v", id, maxTagSuggestionFailures, err)
		m.finishTagSuggestion(id)
		m.setTagSuggestionWorkerState("idle", true, "", fmt.Sprintf("gave up on %s after %d attempts: %v", id, maxTagSuggestionFailures, err))
		return false
	}
	log.Printf("tag suggestion worker: retrying meme %s later: %v", id, err)
	m.requeueTagSuggestion(id)
	m.setTagSuggestionWorkerState("retrying", true, "", err.Error())
	return true
}

// unloadTagModelIfIdle frees the model's memory once nothing is left to tag,
// rather than leaving it loaded for Ollama's keep-alive period.
func (m *MemeManager) unloadTagModelIfIdle() {
	m.suggestionQueueMu.Lock()
	idle := len(m.suggestionQueue) == 0
	m.suggestionQueueMu.Unlock()
	if !idle {
		return
	}
	ctx, cancel := context.WithTimeout(m.workerCtx, 30*time.Second)
	defer cancel()
	if err := m.tagSuggester.Unload(ctx); err != nil {
		log.Printf("tag suggestion worker: could not unload the model: %v", err)
		return
	}
	log.Printf("tag suggestion worker: queue is empty, unloaded model %q", m.tagSuggester.Model())
}

func (m *MemeManager) runTagSuggestionWorker() {
	// One check at startup so the admin panel shows whether Ollama is there.
	// After this Ollama is only contacted when there is a meme to tag.
	ctx, cancel := context.WithTimeout(m.workerCtx, 10*time.Second)
	err := m.tagSuggester.Ready(ctx)
	cancel()
	if err != nil {
		m.setTagSuggestionWorkerState("idle", false, "", err.Error())
	} else {
		m.setTagSuggestionWorkerState("idle", true, "", "")
	}

	for {
		memeID, ok := m.dequeueTagSuggestion()
		if !ok {
			return
		}
		if !m.waitForOllama() {
			m.requeueTagSuggestion(memeID)
			return
		}
		retried, stopping := m.processQueuedTagSuggestion(memeID)
		if stopping {
			return
		}
		if retried && !m.waitOrStop(3*time.Second) {
			return
		}
		m.unloadTagModelIfIdle()
	}
}

// processQueuedTagSuggestion tags one dequeued meme as a single unit of
// background work: a restore that pauses the workers waits for it to finish,
// and one that is already running holds it back until the restore is done.
// It reports whether the meme was requeued after a transient failure, and
// whether the workers are stopping.
func (m *MemeManager) processQueuedTagSuggestion(memeID string) (retried, stopping bool) {
	if m.workGate.Paused() {
		// A restore is running, and ReloadAfterRestore re-seeds the queue from
		// the restored database once it is done. Hand this meme back (the
		// reload drops it if it is gone) and sit out the pause, rather than
		// tag a meme that may no longer exist as soon as the gate opens.
		m.requeueTagSuggestion(memeID)
		if !m.enterBackgroundWork() {
			return false, true
		}
		m.leaveBackgroundWork()
		return false, false
	}
	if !m.enterBackgroundWork() {
		m.requeueTagSuggestion(memeID)
		return false, true
	}
	defer m.leaveBackgroundWork()

	m.setTagSuggestionWorkerState("processing", true, memeID, "")
	timeout := 7 * time.Minute
	if m.tagSuggester != nil {
		timeout = m.tagSuggester.Timeout() + (2 * time.Minute)
	}
	ctx, cancel := context.WithTimeout(m.workerCtx, timeout)
	_, err := m.RefreshMemeTagSuggestions(ctx, "", memeID)
	cancel()
	if m.workerCtx.Err() != nil {
		m.requeueTagSuggestion(memeID)
		return false, true
	}
	switch {
	case err == nil:
		m.finishTagSuggestion(memeID)
		m.recordTagSuggestionSuccess()
	case errors.Is(err, errAutoSuggestExhausted):
		log.Printf("tag suggestion worker: exhausted retries for meme %s; marking auto-suggest disabled", memeID)
		m.finishTagSuggestion(memeID)
		m.setTagSuggestionWorkerState("idle", true, "", err.Error())
	case errors.Is(err, os.ErrNotExist), errors.Is(err, tagsuggest.ErrUnsupported):
		log.Printf("tag suggestion worker: dropping meme %s from queue: %v", memeID, err)
		m.finishTagSuggestion(memeID)
		m.setTagSuggestionWorkerState("idle", true, "", err.Error())
	default:
		// A timeout, an Ollama error, or something unexpected. Whether
		// Ollama is still up is rechecked before the next meme.
		return m.retryOrDropTagSuggestion(memeID, err), false
	}
	return false, false
}

func (m *MemeManager) enqueueTagSuggestion(id string) bool {
	m.suggestionQueueMu.Lock()
	defer m.suggestionQueueMu.Unlock()

	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	if _, exists := m.queuedSuggestionIDs[id]; exists {
		return false
	}

	m.queuedSuggestionIDs[id] = struct{}{}
	m.suggestionQueue = append(m.suggestionQueue, id)
	m.suggestionQueueCond.Signal()
	return true
}

// dequeueTagSuggestion blocks until a meme id is available and returns it with
// ok=true. It returns ("", false) once StopBackgroundWorkers has run, so the
// worker loop can exit instead of blocking on the condition variable forever.
func (m *MemeManager) dequeueTagSuggestion() (string, bool) {
	m.suggestionQueueMu.Lock()
	defer m.suggestionQueueMu.Unlock()

	for len(m.suggestionQueue) == 0 {
		if m.suggestionClosing {
			return "", false
		}
		m.suggestionQueueCond.Wait()
	}

	id := m.suggestionQueue[0]
	m.suggestionQueue = m.suggestionQueue[1:]
	return id, true
}

func (m *MemeManager) finishTagSuggestion(id string) {
	m.suggestionQueueMu.Lock()
	defer m.suggestionQueueMu.Unlock()
	id = strings.TrimSpace(id)
	delete(m.queuedSuggestionIDs, id)
	// Whatever the outcome, the meme has left the queue: a later run starts
	// with a clean failure count.
	delete(m.suggestionFailures, id)
}

func (m *MemeManager) requeueTagSuggestion(id string) {
	m.suggestionQueueMu.Lock()
	defer m.suggestionQueueMu.Unlock()

	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	m.suggestionQueue = append(m.suggestionQueue, id)
	m.suggestionQueueCond.Signal()
}

func (m *MemeManager) setTagSuggestionWorkerState(state string, ready bool, currentID string, lastError string) {
	m.suggestionQueueMu.Lock()
	defer m.suggestionQueueMu.Unlock()

	m.suggestionWorkerState = strings.TrimSpace(state)
	m.suggestionWorkerReady = ready
	m.suggestionCurrentID = strings.TrimSpace(currentID)
	if trimmedError := strings.TrimSpace(lastError); trimmedError != "" {
		m.suggestionLastError = trimmedError
	}
}

func (m *MemeManager) recordTagSuggestionSuccess() {
	m.suggestionQueueMu.Lock()
	defer m.suggestionQueueMu.Unlock()

	m.suggestionWorkerState = "idle"
	m.suggestionWorkerReady = true
	m.suggestionCurrentID = ""
	m.suggestionLastSuccess = time.Now().UTC()
}

// DismissMemeTagSuggestion drops one tag from a meme's suggestions. The
// removal runs against the stored list (RemoveSuggestedTag), not the meme read
// here, so a refresh that lands in between keeps its new suggestions.
func (m *MemeManager) DismissMemeTagSuggestion(ctx context.Context, userID string, id string, tag string) (accessor.Meme, error) {
	suggestionStore, ok := m.store.(accessor.SuggestedTagStore)
	if !ok {
		return accessor.Meme{}, tagsuggest.ErrDisabled
	}

	// Read first only to check the meme exists and this user can see it.
	meme, err := m.store.GetByID(ctx, strings.TrimSpace(userID), strings.TrimSpace(id))
	if err != nil {
		return accessor.Meme{}, err
	}

	if err := suggestionStore.RemoveSuggestedTag(ctx, meme.ID, tag); err != nil {
		return accessor.Meme{}, err
	}

	return m.store.GetByID(ctx, strings.TrimSpace(userID), meme.ID)
}

// DismissAllMemeTagSuggestions clears a meme's suggestions unconditionally.
// Unlike dismissing one tag this writes nothing derived from the meme it read,
// so it cannot write back a stale list: if a refresh lands at the same moment,
// the result is what running the two one after the other would give (the
// refresh's list if it stores last, an empty list if the clear does). Clearing
// only when the list still matches what the client was shown would need the
// client to send that list, and buys little - suggestions are advisory and a
// refresh is one click away.
func (m *MemeManager) DismissAllMemeTagSuggestions(ctx context.Context, userID string, id string) (accessor.Meme, error) {
	suggestionStore, ok := m.store.(accessor.SuggestedTagStore)
	if !ok {
		return accessor.Meme{}, tagsuggest.ErrDisabled
	}

	meme, err := m.store.GetByID(ctx, strings.TrimSpace(userID), strings.TrimSpace(id))
	if err != nil {
		return accessor.Meme{}, err
	}
	if err := suggestionStore.ReplaceSuggestedTags(ctx, meme.ID, nil); err != nil {
		return accessor.Meme{}, err
	}

	return m.store.GetByID(ctx, strings.TrimSpace(userID), meme.ID)
}

// ApplyMemeTagSuggestion adds one suggested tag to a meme and drops it from
// the meme's suggestions. The tag is added as a delta, so tags, notes and
// favorites changed by anyone else in the meantime are left as they are, and
// the suggestion is removed from the stored list in one write
// (RemoveSuggestedTag), so a suggestion refresh that lands in between is not
// overwritten with the list this read before it.
func (m *MemeManager) ApplyMemeTagSuggestion(ctx context.Context, userID string, id string, tag string, actor accessor.AuditActor) (accessor.Meme, error) {
	normalizedTag := strings.ToLower(strings.TrimSpace(tag))
	if normalizedTag == "" {
		return m.store.GetByID(ctx, strings.TrimSpace(userID), strings.TrimSpace(id))
	}

	// Adding the tag and dropping the suggestion are separate writes. A
	// request cancelled between them would leave the tag applied but still
	// suggested, keeping the meme in the review queue, so once started both
	// run to the end.
	ctx = context.WithoutCancel(ctx)
	meme, err := m.EditMeme(ctx, userID, id, accessor.MemeEdit{
		TagsAdded: []string{normalizedTag},
		Actor:     actor,
	})
	if err != nil {
		return accessor.Meme{}, err
	}

	suggestionStore, ok := m.store.(accessor.SuggestedTagStore)
	if ok {
		if err := suggestionStore.RemoveSuggestedTag(ctx, meme.ID, normalizedTag); err != nil {
			return accessor.Meme{}, err
		}
	}

	return m.store.GetByID(ctx, strings.TrimSpace(userID), meme.ID)
}

// SweepTagScratch removes leftover tag-suggestion working files from the
// thumbnails directory and reports how many were deleted. Call it before the
// suggestion worker starts so it cannot delete the files of a suggestion in
// flight.
func (m *MemeManager) SweepTagScratch() int {
	return accessor.SweepTagScratch(m.ThumbnailDir())
}

// StartTagSuggestionWorkerAfterSweep runs SweepTagScratch in the background
// and starts the tag suggestion worker once it is done, so the sweep never
// deletes the files of a suggestion in flight. The first sweep after an
// upgrade can have tens of thousands of files to delete, which is why it is
// off the startup path. The sweep is worker-tracked and one unit of
// background work: a restore, which swaps the thumbnails directory, waits for
// it or holds it back. If the workers stop before or during the sweep, the
// suggestion worker is never started.
func (m *MemeManager) StartTagSuggestionWorkerAfterSweep() {
	m.workerWG.Add(1)
	go func() {
		defer m.workerWG.Done()
		if m.workerCtx.Err() != nil || !m.enterBackgroundWork() {
			return
		}
		removed := m.SweepTagScratch()
		m.leaveBackgroundWork()
		if removed > 0 {
			log.Printf("tag suggestion scratch: removed %d leftover working file(s) from the thumbnails folder", removed)
		}
		if m.workerCtx.Err() != nil {
			return
		}
		// Still inside this tracked goroutine, so StopBackgroundWorkers is
		// waiting on it and will wait on the worker too.
		m.StartTagSuggestionWorker()
	}()
}

func (m *MemeManager) suggestionRequestForMeme(ctx context.Context, meme accessor.Meme, options tagSuggestionAttemptOptions) (tagsuggest.Request, error) {
	switch {
	case strings.HasPrefix(meme.ContentType, "image/"):
		imagePath := filepath.Join(m.store.UploadDir(), meme.StoredName)
		if previewStore, ok := m.store.(accessor.PreviewAssetStore); ok {
			thumbnailDir := strings.TrimSpace(previewStore.ThumbnailDir())
			if thumbnailDir != "" {
				resizedPath, err := accessor.EnsureImageTagPreview(m.store.UploadDir(), thumbnailDir, meme.StoredName, options.imageWidth)
				if err == nil {
					imagePath = resizedPath
				}
			}
		}
		if _, err := os.Stat(imagePath); err != nil {
			return tagsuggest.Request{}, tagsuggest.ErrUnavailable
		}
		return tagsuggest.Request{
			AssetPaths: []string{imagePath},
			Source:     "image-preview" + options.sourceSuffix,
		}, nil
	case strings.HasPrefix(meme.ContentType, "video/"):
		previewStore, ok := m.store.(accessor.PreviewAssetStore)
		if !ok {
			return tagsuggest.Request{}, tagsuggest.ErrUnsupported
		}
		thumbnailDir := strings.TrimSpace(previewStore.ThumbnailDir())
		if thumbnailDir == "" {
			return tagsuggest.Request{}, tagsuggest.ErrUnsupported
		}
		framePaths, err := accessor.EnsureVideoTagFrames(m.store.UploadDir(), thumbnailDir, meme.StoredName, options.videoFrameCount, options.videoFrameWidth)
		if err != nil {
			return tagsuggest.Request{}, tagsuggest.ErrUnavailable
		}
		request := tagsuggest.Request{
			AssetPaths: framePaths,
			Source:     "video-frames" + options.sourceSuffix,
		}
		if options.includeTranscript {
			if transcript := m.transcriptForVideo(ctx, meme, thumbnailDir); transcript != "" {
				request.Transcript = transcript
				request.Source = "video-frames+audio-transcript" + options.sourceSuffix
			}
		}
		return request, nil
	default:
		return tagsuggest.Request{}, tagsuggest.ErrUnsupported
	}
}

func (m *MemeManager) transcriptForVideo(ctx context.Context, meme accessor.Meme, thumbnailDir string) string {
	if m.disableTranscript {
		return ""
	}
	if m.transcriber == nil || !m.transcriber.Enabled() {
		return ""
	}

	audioPath, err := accessor.EnsureVideoTagAudio(m.store.UploadDir(), thumbnailDir, meme.StoredName)
	if err != nil {
		log.Printf("tag suggestion worker: audio extraction skipped for meme %s: %v", meme.ID, err)
		return ""
	}

	transcript, err := m.transcriber.Transcribe(ctx, audioPath)
	if err != nil {
		log.Printf("tag suggestion worker: transcription skipped for meme %s: %v", meme.ID, err)
		return ""
	}
	return transcript
}

func (m *MemeManager) suggestTagsForMeme(ctx context.Context, meme accessor.Meme) (tagsuggest.Result, error) {
	attempts := m.tagSuggestionAttemptsForMeme(meme)
	// The frames, resized image and audio written for the model are working
	// files; leaving them behind grows the thumbnails folder without bound.
	defer accessor.RemoveTagScratch(m.ThumbnailDir(), meme.StoredName)
	var lastUnavailable error
	var lastInvalidResponse error

	for _, attempt := range attempts {
		request, err := m.suggestionRequestForMeme(ctx, meme, attempt)
		if err != nil {
			if errors.Is(err, tagsuggest.ErrUnavailable) {
				lastUnavailable = err
				continue
			}
			return tagsuggest.Result{}, err
		}

		request.Filename = meme.OriginalName
		request.ContentType = meme.ContentType
		request.ExistingTags = meme.Tags
		request.KnownTags = m.store.SuggestTags(ctx, "", m.knownTagHint)

		result, err := m.tagSuggester.Suggest(ctx, request)
		if err == nil {
			return result, nil
		}
		if errors.Is(err, tagsuggest.ErrUnavailable) {
			lastUnavailable = err
			continue
		}
		if errors.Is(err, tagsuggest.ErrInvalidResponse) {
			lastInvalidResponse = err
			continue
		}
		return tagsuggest.Result{}, err
	}

	if lastInvalidResponse != nil {
		if suggestionStore, ok := m.store.(accessor.SuggestedTagStore); ok {
			// The model's verdict is in; record it even if the context ran out
			// while the last attempt was answering (see RefreshMemeTagSuggestions).
			if err := suggestionStore.SetAutoSuggestDisabled(context.WithoutCancel(ctx), meme.ID, true); err != nil {
				return tagsuggest.Result{}, err
			}
		}
		return tagsuggest.Result{}, fmt.Errorf("%w: %w", errAutoSuggestExhausted, lastInvalidResponse)
	}
	if lastUnavailable != nil {
		return tagsuggest.Result{}, lastUnavailable
	}

	return tagsuggest.Result{}, tagsuggest.ErrUnavailable
}

func (m *MemeManager) tagSuggestionAttemptsForMeme(meme accessor.Meme) []tagSuggestionAttemptOptions {
	attempts := []tagSuggestionAttemptOptions{}
	seen := map[string]struct{}{}
	addAttempt := func(imageWidth, frameCount, frameWidth int, includeTranscript bool, suffix string) {
		key := fmt.Sprintf("%d|%d|%d|%t", imageWidth, frameCount, frameWidth, includeTranscript)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		attempts = append(attempts, tagSuggestionAttemptOptions{
			imageWidth:        imageWidth,
			videoFrameCount:   frameCount,
			videoFrameWidth:   frameWidth,
			includeTranscript: includeTranscript,
			sourceSuffix:      suffix,
		})
	}

	includeTranscript := !m.disableTranscript
	if strings.HasPrefix(meme.ContentType, "image/") {
		addAttempt(max(m.videoFrameWidth, 320), 1, max(m.videoFrameWidth, 320), false, "")
		return attempts
	}
	addAttempt(max(m.videoFrameWidth, 320), max(m.videoFrameCount, 1), max(m.videoFrameWidth, 320), includeTranscript, "")
	addAttempt(320, 1, 320, false, "+fast")
	addAttempt(240, 1, 240, false, "+minimal")
	return attempts
}

func shouldQueueTagSuggestionsForMeme(meme accessor.Meme) bool {
	return len(meme.Tags) == 0 && len(meme.SuggestedTags) == 0 && !meme.AutoSuggestDisabled
}
