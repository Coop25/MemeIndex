package manager

import (
	"context"
	"io"
	"log"
	"net/textproto"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"memeindex/internal/accessor"
	"memeindex/internal/tagsuggest"
	"memeindex/internal/workgate"
)

type MemeManager struct {
	store             accessor.Store
	reelSessions      *ReelSessionStore
	tagSuggester      *tagsuggest.Service
	transcriber       *tagsuggest.Transcriber
	knownTagHint      int
	videoFrameCount   int
	videoFrameWidth   int
	disableTranscript bool

	suggestionQueueMu     sync.Mutex
	suggestionQueueCond   *sync.Cond
	suggestionQueue       []string
	queuedSuggestionIDs   map[string]struct{}
	suggestionFailures    map[string]int // transient failures per queued meme id
	suggestionClosing     bool           // set under suggestionQueueMu by StopBackgroundWorkers
	suggestionWorkerStart sync.Once
	suggestionWorkerReady bool
	suggestionWorkerState string
	suggestionCurrentID   string
	suggestionLastError   string
	suggestionLastSuccess time.Time

	// Background-worker lifecycle. workerCtx is cancelled by
	// StopBackgroundWorkers so the tag-hygiene and tag-suggestion loops exit
	// their waits promptly during a graceful shutdown; workerWG lets the caller
	// block until they have actually returned.
	workerCtx      context.Context
	workerCancel   context.CancelFunc
	workerWG       sync.WaitGroup
	stopWorkersOne sync.Once
	// workGate holds the background workers still while a backup restore or
	// migration replaces the database and media underneath them (see
	// PauseBackgroundWork). Each meme the suggestion worker tags, each
	// tag-hygiene recompute and each reel-session cleanup is one unit of work.
	workGate workgate.Gate

	// adminReadMu guards short-lived memoization of the two admin read models
	// that each require a full store scan. This is a burst coalescer, not a
	// freshness cache: entries expire after AdminReadCacheTTL and are dropped
	// outright on ReloadAfterRestore. Deep copies are handed out so callers
	// (which rewrite asset paths per request) never touch the stored value.
	adminReadMu            sync.Mutex
	adminDashboardCache    *AdminDashboardStats
	adminDashboardCachedAt time.Time
	tagHygieneCache        *TagHygieneReport
	tagHygieneCachedAt     time.Time
	tagHygieneDirty        bool
	tagHygieneRefresh      chan struct{}
	tagHygieneWorkerStart  sync.Once

	// hiddenMemeIDs caches the ids of memes hidden pending delete approval, so
	// the /uploads/ and /thumbnails/ file handlers can refuse their files
	// without a database round-trip per asset. nil means "reload on next use";
	// every path that hides or unhides a meme resets it.
	hiddenMemeMu  sync.Mutex
	hiddenMemeIDs map[string]struct{}
}

type TagSuggestionRuntimeConfig struct {
	VideoFrameCount   int
	VideoFrameWidth   int
	DisableTranscript bool
}

func NewMemeManager(store accessor.Store) *MemeManager {
	return NewMemeManagerWithTagSuggester(store, nil, nil, TagSuggestionRuntimeConfig{}, 60)
}

func NewMemeManagerWithTagSuggester(store accessor.Store, tagSuggester *tagsuggest.Service, transcriber *tagsuggest.Transcriber, runtimeConfig TagSuggestionRuntimeConfig, knownTagHint int) *MemeManager {
	sessionFile := filepath.Join(filepath.Dir(store.UploadDir()), "reel_sessions.json")
	if knownTagHint <= 0 {
		knownTagHint = 60
	}
	if runtimeConfig.VideoFrameCount <= 0 {
		runtimeConfig.VideoFrameCount = 3
	}
	if runtimeConfig.VideoFrameWidth <= 0 {
		runtimeConfig.VideoFrameWidth = 480
	}
	manager := &MemeManager{
		store:               store,
		reelSessions:        NewReelSessionStore(store, sessionFile),
		tagSuggester:        tagSuggester,
		transcriber:         transcriber,
		knownTagHint:        knownTagHint,
		videoFrameCount:     runtimeConfig.VideoFrameCount,
		videoFrameWidth:     runtimeConfig.VideoFrameWidth,
		disableTranscript:   runtimeConfig.DisableTranscript,
		queuedSuggestionIDs: map[string]struct{}{},
		suggestionFailures:  map[string]int{},
		suggestionWorkerState: func() string {
			if tagSuggester == nil || !tagSuggester.Enabled() {
				return "disabled"
			}
			return "starting"
		}(),
	}
	manager.suggestionQueueCond = sync.NewCond(&manager.suggestionQueueMu)
	manager.workerCtx, manager.workerCancel = context.WithCancel(context.Background())
	return manager
}

// StopBackgroundWorkers signals the tag-hygiene and tag-suggestion workers to
// stop and waits up to timeout for them to return. It is safe to call once;
// further calls are no-ops. A long-running tag-suggestion request in flight is
// cancelled via the worker context, but if it does not unwind within the
// timeout this returns anyway so shutdown can proceed.
func (m *MemeManager) StopBackgroundWorkers(timeout time.Duration) {
	m.stopWorkersOne.Do(func() {
		m.workerCancel()
		m.suggestionQueueMu.Lock()
		m.suggestionClosing = true
		m.suggestionQueueMu.Unlock()
		m.suggestionQueueCond.Broadcast()
	})

	done := make(chan struct{})
	go func() {
		m.workerWG.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		log.Printf("background workers did not stop within %s; continuing shutdown", timeout)
	}
}

// waitOrStop blocks for d, or returns false early if the workers are stopping.
func (m *MemeManager) waitOrStop(d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-m.workerCtx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// PauseBackgroundWork holds the tag-suggestion and tag-hygiene workers and
// the reel-session cleanup still, for a restore that is about to replace the
// database. It waits for the unit of work each has in flight to finish (or
// for ctx to end, which lifts the pause and returns ctx's error), and returns
// the function that lets them run again. Queued work is kept, not dropped;
// ReloadAfterRestore re-seeds the suggestion queue before resuming.
func (m *MemeManager) PauseBackgroundWork(ctx context.Context) (resume func(), err error) {
	return m.workGate.Pause(ctx)
}

// enterBackgroundWork waits out any restore pause before a unit of
// background work. It reports false if the workers are stopping instead; on
// true the caller must call leaveBackgroundWork when the unit is done.
func (m *MemeManager) enterBackgroundWork() bool {
	return m.workGate.Enter(m.workerCtx.Done())
}

func (m *MemeManager) leaveBackgroundWork() {
	m.workGate.Leave()
}

func (m *MemeManager) CreateMeme(file io.Reader, header textproto.MIMEHeader, filename string, tags []string, notes string) (accessor.Meme, error) {
	return m.store.Create(accessor.CreateInput{
		File:     file,
		Header:   header,
		Filename: filename,
		Tags:     normalizeTags(tags),
		Notes:    strings.TrimSpace(notes),
	})
}

func (m *MemeManager) CreateMemeAs(actor accessor.AuditActor, file io.Reader, header textproto.MIMEHeader, filename string, tags []string, notes string) (accessor.Meme, error) {
	return m.CreateMemeAsWithSource(actor, file, header, filename, tags, notes, "")
}

func (m *MemeManager) CreateMemeAsWithSource(actor accessor.AuditActor, file io.Reader, header textproto.MIMEHeader, filename string, tags []string, notes string, sourceURL string) (accessor.Meme, error) {
	meme, err := m.store.Create(accessor.CreateInput{
		File:      file,
		Header:    header,
		Filename:  filename,
		Tags:      normalizeTags(tags),
		Notes:     strings.TrimSpace(notes),
		SourceURL: strings.TrimSpace(sourceURL),
		Actor:     actor,
	})
	if err == nil {
		m.markTagsChanged()
	}
	return meme, err
}

// EditMeme applies a partial edit to one meme. The store works out what the
// edit changes against the meme's current state with its row locked, and runs
// edit.Authorize on that change before writing, so a permission check made
// there cannot be invalidated by a concurrent edit.
func (m *MemeManager) EditMeme(userID, id string, edit accessor.MemeEdit) (accessor.Meme, error) {
	meme, err := m.store.EditMeme(strings.TrimSpace(userID), strings.TrimSpace(id), edit)
	if err == nil {
		m.markTagsChanged()
	}
	return meme, err
}

func (m *MemeManager) SetFavorite(userID, id string, favorite bool) (accessor.Meme, error) {
	return m.store.SetFavorite(strings.TrimSpace(userID), strings.TrimSpace(id), favorite)
}

func (m *MemeManager) SetFavoriteAs(userID, id string, favorite bool, actor accessor.AuditActor) (accessor.Meme, error) {
	if store, ok := m.store.(accessor.AuditStore); ok {
		return store.SetFavoriteWithActor(strings.TrimSpace(userID), strings.TrimSpace(id), favorite, actor)
	}
	return m.SetFavorite(userID, id, favorite)
}

func (m *MemeManager) GetMeme(userID, id string) (accessor.Meme, error) {
	return m.store.GetByID(strings.TrimSpace(userID), strings.TrimSpace(id))
}

// memesByIDs resolves visible memes by id in one batch where the store supports
// it (AdminMemeStore), falling back to per-id lookups otherwise. Missing ids are
// simply absent from the map.
func (m *MemeManager) memesByIDs(ids []string) map[string]accessor.Meme {
	if bulk, ok := m.store.(accessor.AdminMemeStore); ok {
		memes, err := bulk.MemesByIDs(ids)
		if err == nil {
			return memes
		}
		log.Printf("memes by ids: SQL batch failed, falling back to per-id reads: %v", err)
	}
	out := make(map[string]accessor.Meme, len(ids))
	for _, id := range ids {
		if meme, err := m.store.GetByID("", strings.TrimSpace(id)); err == nil {
			out[meme.ID] = meme
		}
	}
	return out
}

// memeNamesByIDs is memesByIDs reduced to just the original display name per id.
func (m *MemeManager) memeNamesByIDs(ids []string) map[string]string {
	memes := m.memesByIDs(ids)
	names := make(map[string]string, len(memes))
	for id, meme := range memes {
		names[id] = meme.OriginalName
	}
	return names
}

func (m *MemeManager) GetAdminMeme(id string) (accessor.Meme, error) {
	store, ok := m.store.(accessor.AdminMemeStore)
	if ok {
		return store.GetAnyByID(strings.TrimSpace(id))
	}
	return m.store.GetByID("", strings.TrimSpace(id))
}

func (m *MemeManager) DeleteMeme(id string, actor accessor.AuditActor) (accessor.DeleteResult, error) {
	result, err := m.store.Delete(accessor.DeleteInput{
		ID:    strings.TrimSpace(id),
		Actor: actor,
	})
	if err == nil {
		m.markTagsChanged()
		m.invalidateHiddenMemes()
	}
	return result, err
}

func (m *MemeManager) SuggestTags(prefix string, limit int) []string {
	return m.store.SuggestTags(strings.TrimSpace(prefix), limit)
}

// ReloadAfterRestore refreshes the small pieces of state cached in memory after
// an administrator replaces the persistent database with a portable backup.
func (m *MemeManager) ReloadAfterRestore() error {
	m.invalidateHiddenMemes()
	if err := m.reelSessions.load(); err != nil {
		return err
	}

	m.suggestionQueueMu.Lock()
	m.suggestionQueue = nil
	m.queuedSuggestionIDs = map[string]struct{}{}
	if m.suggestionCurrentID != "" {
		m.queuedSuggestionIDs[m.suggestionCurrentID] = struct{}{}
	}
	m.suggestionQueueMu.Unlock()
	m.SeedTagSuggestionQueue()
	m.invalidateAdminReadCache()
	// The restore swapped the thumbnails directory; forget which thumbnails we
	// have seen so preview paths are re-derived against the new contents.
	accessor.ResetPreviewPathCache()
	return nil
}

func (m *MemeManager) RandomMeme(excludedIDs []string) (accessor.Meme, error) {
	normalized := make([]string, 0, len(excludedIDs))
	for _, id := range excludedIDs {
		trimmed := strings.TrimSpace(id)
		if trimmed == "" {
			continue
		}
		normalized = append(normalized, trimmed)
	}
	return m.store.Random(normalized)
}

func (m *MemeManager) StepRandomReel(sessionID string, direction string) (ReelStepResult, error) {
	return m.reelSessions.Step(sessionID, direction)
}

func (m *MemeManager) DeleteRandomReelSession(sessionID string) error {
	return m.reelSessions.Delete(sessionID)
}

// CleanupStaleReelSessions runs the nightly reel-session cleanup. It waits
// for a restore in progress to finish first, and does nothing if the workers
// are stopping in the meantime.
func (m *MemeManager) CleanupStaleReelSessions() error {
	if !m.enterBackgroundWork() {
		return nil
	}
	defer m.leaveBackgroundWork()
	return m.reelSessions.CleanupStale()
}

func (m *MemeManager) UploadDir() string {
	return m.store.UploadDir()
}

// Store exposes the backing store for read-only introspection such as the
// /readyz health probe. Callers must not mutate meme state through it.
func (m *MemeManager) Store() accessor.Store {
	return m.store
}

func (m *MemeManager) ThumbnailDir() string {
	previewStore, ok := m.store.(accessor.PreviewAssetStore)
	if !ok {
		return ""
	}
	return previewStore.ThumbnailDir()
}

func (m *MemeManager) EnsurePreviewAssets() error {
	previewStore, ok := m.store.(accessor.PreviewAssetStore)
	if !ok {
		return nil
	}
	return previewStore.EnsurePreviewAssets()
}

func normalizeTags(tags []string) []string {
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		trimmed := strings.ToLower(strings.TrimSpace(tag))
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func containsTag(tags []string, needle string) bool {
	normalized := strings.ToLower(strings.TrimSpace(needle))
	if normalized == "" {
		return false
	}
	for _, tag := range tags {
		if strings.ToLower(strings.TrimSpace(tag)) == normalized {
			return true
		}
	}
	return false
}

func removeTagValue(tags []string, remove string) []string {
	normalizedRemove := strings.ToLower(strings.TrimSpace(remove))
	if normalizedRemove == "" {
		return normalizeTags(tags)
	}

	filtered := make([]string, 0, len(tags))
	for _, tag := range tags {
		normalizedTag := strings.ToLower(strings.TrimSpace(tag))
		if normalizedTag == "" || normalizedTag == normalizedRemove {
			continue
		}
		filtered = append(filtered, normalizedTag)
	}
	return normalizeTags(filtered)
}
