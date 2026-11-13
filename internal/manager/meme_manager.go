package manager

import (
	"context"
	"errors"
	"io"
	"log"
	"math/rand/v2"
	"net/textproto"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"memeindex/internal/accessor"
	"memeindex/internal/tagsuggest"
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

const AdminReadCacheTTL = 5 * time.Second

type TagSuggestionRuntimeConfig struct {
	VideoFrameCount   int
	VideoFrameWidth   int
	DisableTranscript bool
}

type MemeCounts struct {
	Total     int `json:"total"`
	Favorites int `json:"favorites"`
	Videos    int `json:"videos"`
	Images    int `json:"images"`
	MP3s      int `json:"mp3s"`
	Untagged  int `json:"untagged"`
	Files     int `json:"files"`
}

type MemeListResult struct {
	Memes      []accessor.Meme `json:"memes"`
	Counts     MemeCounts      `json:"counts"`
	HasMore    bool            `json:"has_more"`
	NextOffset int             `json:"next_offset"`
}

// VaultDashboard is intentionally derived from the current store on every request.
// It keeps the user home screen honest without introducing a second statistics
// cache that could drift from the archive.
type VaultDashboard struct {
	TotalItems    int             `json:"total_items"`
	Favorites     int             `json:"favorites"`
	StorageBytes  int64           `json:"storage_bytes"`
	TagCount      int             `json:"tag_count"`
	Counts        MemeCounts      `json:"counts"`
	RecentItems   []accessor.Meme `json:"recent_items"`
	FavoriteItems []accessor.Meme `json:"favorite_items"`
	RandomItems   []accessor.Meme `json:"random_items"`
	TopTags       []VaultTagStat  `json:"top_tags"`
}

type VaultTagStat struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
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

// maxMemePageLimit caps a single browse/search page so a caller-supplied limit
// cannot ask the database (or the in-memory fallback) for an unbounded slice.
const maxMemePageLimit = 500

func (m *MemeManager) ListMemes(userID, query string, favoritesOnly bool, tag string, view string, offset int, limit int) MemeListResult {
	return m.ListMemesSorted(userID, query, favoritesOnly, tag, view, "newest", offset, limit)
}

func (m *MemeManager) ListMemesSorted(userID, query string, favoritesOnly bool, tag string, view string, sortBy string, offset int, limit int) MemeListResult {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 72
	}
	if limit > maxMemePageLimit {
		limit = maxMemePageLimit
	}

	if qs, ok := m.store.(accessor.QueryableMemeStore); ok {
		page, err := qs.QueryMemes(accessor.MemeQuery{
			UserID:        strings.TrimSpace(userID),
			Search:        strings.TrimSpace(query),
			Tag:           strings.TrimSpace(tag),
			View:          strings.TrimSpace(view),
			FavoritesOnly: favoritesOnly,
			Sort:          sortBy,
			Offset:        offset,
			Limit:         limit,
		})
		if err == nil {
			memes := page.Memes
			if memes == nil {
				memes = []accessor.Meme{}
			}
			return MemeListResult{
				Memes:      memes,
				Counts:     memeCountsFromCategory(page.Counts),
				HasMore:    page.HasMore,
				NextOffset: page.NextOffset,
			}
		}
		log.Printf("meme list: SQL query failed, falling back to in-memory scan: %v", err)
	}

	source := m.store.List(strings.TrimSpace(userID), strings.TrimSpace(query), false, strings.TrimSpace(tag))
	counts := buildMemeCounts(source)
	visible := filterMemesByView(source, strings.TrimSpace(view))
	if favoritesOnly {
		visible = filterMemesByView(visible, "favorites")
	}
	sortMemes(visible, sortBy)

	start := min(offset, len(visible))
	end := min(offset+limit, len(visible))
	page := visible[start:end]

	return MemeListResult{
		Memes:      append([]accessor.Meme(nil), page...),
		Counts:     counts,
		HasMore:    end < len(visible),
		NextOffset: end,
	}
}

func memeCountsFromCategory(c accessor.MemeCategoryCounts) MemeCounts {
	return MemeCounts{
		Total:     c.Total,
		Favorites: c.Favorites,
		Videos:    c.Videos,
		Images:    c.Images,
		MP3s:      c.MP3s,
		Untagged:  c.Untagged,
		Files:     c.Files,
	}
}

func (m *MemeManager) Dashboard(userID string) VaultDashboard {
	if qs, ok := m.store.(accessor.QueryableMemeStore); ok {
		data, err := qs.MemeDashboard(strings.TrimSpace(userID))
		if err == nil {
			return vaultDashboardFromData(data)
		}
		log.Printf("dashboard: SQL query failed, falling back to in-memory scan: %v", err)
	}

	items := m.store.List(strings.TrimSpace(userID), "", false, "")
	dashboard := VaultDashboard{
		Counts:        buildMemeCounts(items),
		RecentItems:   []accessor.Meme{},
		FavoriteItems: []accessor.Meme{},
		RandomItems:   []accessor.Meme{},
		TopTags:       []VaultTagStat{},
	}
	tagCounts := map[string]int{}
	for _, item := range items {
		dashboard.TotalItems++
		dashboard.StorageBytes += item.SizeBytes
		if item.Favorite {
			dashboard.Favorites++
			if len(dashboard.FavoriteItems) < accessor.DashboardFavoriteLimit {
				dashboard.FavoriteItems = append(dashboard.FavoriteItems, item)
			}
		}
		for _, tag := range item.Tags {
			normalized := strings.TrimSpace(tag)
			if normalized != "" {
				tagCounts[normalized]++
			}
		}
	}
	limit := min(6, len(items))
	dashboard.RecentItems = append(dashboard.RecentItems, items[:min(accessor.DashboardRecentLimit, len(items))]...)
	for _, index := range rand.Perm(len(items))[:limit] {
		dashboard.RandomItems = append(dashboard.RandomItems, items[index])
	}
	dashboard.TagCount = len(tagCounts)
	dashboard.TopTags = topTagStats(tagCounts, accessor.DashboardTopTagLimit)
	return dashboard
}

// NewestMemeID returns the id of the most recently added meme the user can
// see, or "" for an empty vault. It is the cheap check behind the home
// screen's "anything new?" poll.
func (m *MemeManager) NewestMemeID(userID string) string {
	page := m.ListMemesSorted(userID, "", false, "", "", "newest", 0, 1)
	if len(page.Memes) == 0 {
		return ""
	}
	return page.Memes[0].ID
}

func vaultDashboardFromData(data accessor.MemeDashboardData) VaultDashboard {
	dashboard := VaultDashboard{
		TotalItems:    data.TotalItems,
		Favorites:     data.Favorites,
		StorageBytes:  data.StorageBytes,
		TagCount:      data.TagCount,
		Counts:        memeCountsFromCategory(data.Counts),
		RecentItems:   nonNilMemes(data.RecentItems),
		FavoriteItems: nonNilMemes(data.FavoriteItems),
		RandomItems:   nonNilMemes(data.RandomItems),
		TopTags:       vaultTagStats(data.TopTags),
	}
	return dashboard
}

func nonNilMemes(memes []accessor.Meme) []accessor.Meme {
	if memes == nil {
		return []accessor.Meme{}
	}
	return memes
}

func vaultTagStats(tags []accessor.TagCount) []VaultTagStat {
	stats := make([]VaultTagStat, 0, len(tags))
	for _, tag := range tags {
		stats = append(stats, VaultTagStat{Name: tag.Name, Count: tag.Count})
	}
	return stats
}

func (m *MemeManager) PopularTags(userID string, limit int) []VaultTagStat {
	if limit <= 0 {
		limit = 10
	}
	limit = min(limit, 50)

	if qs, ok := m.store.(accessor.QueryableMemeStore); ok {
		tags, err := qs.TagCounts(limit)
		if err == nil {
			return vaultTagStats(tags)
		}
		log.Printf("popular tags: SQL query failed, falling back to in-memory scan: %v", err)
	}

	tagCounts := map[string]int{}
	for _, item := range m.store.List(strings.TrimSpace(userID), "", false, "") {
		for _, tag := range item.Tags {
			normalized := strings.TrimSpace(tag)
			if normalized != "" {
				tagCounts[normalized]++
			}
		}
	}
	return topTagStats(tagCounts, limit)
}

func topTagStats(tagCounts map[string]int, limit int) []VaultTagStat {
	stats := make([]VaultTagStat, 0, len(tagCounts))
	for name, count := range tagCounts {
		stats = append(stats, VaultTagStat{Name: name, Count: count})
	}
	slices.SortFunc(stats, func(a, b VaultTagStat) int {
		if a.Count != b.Count {
			return b.Count - a.Count
		}
		return strings.Compare(a.Name, b.Name)
	})
	if limit >= 0 && len(stats) > limit {
		stats = stats[:limit]
	}
	return stats
}

func sortMemes(memes []accessor.Meme, sortBy string) {
	switch strings.ToLower(strings.TrimSpace(sortBy)) {
	case "oldest":
		slices.SortStableFunc(memes, func(a, b accessor.Meme) int { return a.CreatedAt.Compare(b.CreatedAt) })
	case "name":
		slices.SortStableFunc(memes, func(a, b accessor.Meme) int {
			return strings.Compare(strings.ToLower(a.OriginalName), strings.ToLower(b.OriginalName))
		})
	case "size":
		slices.SortStableFunc(memes, func(a, b accessor.Meme) int {
			if a.SizeBytes == b.SizeBytes {
				return 0
			}
			if a.SizeBytes > b.SizeBytes {
				return -1
			}
			return 1
		})
	case "updated":
		slices.SortStableFunc(memes, func(a, b accessor.Meme) int { return b.UpdatedAt.Compare(a.UpdatedAt) })
	default:
		slices.SortStableFunc(memes, func(a, b accessor.Meme) int { return b.CreatedAt.Compare(a.CreatedAt) })
	}
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

func (m *MemeManager) UpdateMeme(userID, id string, update accessor.MemeUpdate) (accessor.Meme, error) {
	update.Tags = normalizeTags(update.Tags)
	update.Notes = strings.TrimSpace(update.Notes)
	meme, err := m.store.Update(strings.TrimSpace(userID), strings.TrimSpace(id), update)
	if err == nil {
		m.markTagsChanged()
	}
	return meme, err
}

func (m *MemeManager) SetFavorite(userID, id string, favorite bool) (accessor.Meme, error) {
	return m.store.SetFavorite(strings.TrimSpace(userID), strings.TrimSpace(id), favorite)
}

func (m *MemeManager) SetFavoriteAs(userID, id string, favorite bool, actor accessor.AuditActor) (accessor.Meme, error) {
	if store, ok := m.store.(accessor.FavoriteAuditStore); ok {
		return store.SetFavoriteWithActor(strings.TrimSpace(userID), strings.TrimSpace(id), favorite, actor)
	}
	return m.SetFavorite(userID, id, favorite)
}

func (m *MemeManager) GetMeme(userID, id string) (accessor.Meme, error) {
	return m.store.GetByID(strings.TrimSpace(userID), strings.TrimSpace(id))
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

func (m *MemeManager) CleanupStaleReelSessions() error {
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

// SweepTagScratch removes leftover tag-suggestion working files from the
// thumbnails directory and reports how many were deleted. Call it before the
// suggestion worker starts so it cannot delete the files of a suggestion in
// flight.
func (m *MemeManager) SweepTagScratch() int {
	return accessor.SweepTagScratch(m.ThumbnailDir())
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

func buildMemeCounts(memes []accessor.Meme) MemeCounts {
	counts := MemeCounts{}
	for _, meme := range memes {
		counts.Total += 1
		if meme.Favorite {
			counts.Favorites += 1
		}
		switch {
		case strings.HasPrefix(meme.ContentType, "video/"):
			counts.Videos += 1
		case strings.HasPrefix(meme.ContentType, "image/"):
			counts.Images += 1
		case meme.ContentType == "audio/mpeg" || strings.HasSuffix(strings.ToLower(meme.OriginalName), ".mp3"):
			counts.MP3s += 1
		default:
			counts.Files += 1
		}
		if len(meme.Tags) == 0 {
			counts.Untagged += 1
		}
	}
	return counts
}

func filterMemesByView(memes []accessor.Meme, view string) []accessor.Meme {
	switch view {
	case "favorites":
		return filterMemes(memes, func(meme accessor.Meme) bool { return meme.Favorite })
	case "videos":
		return filterMemes(memes, func(meme accessor.Meme) bool { return strings.HasPrefix(meme.ContentType, "video/") })
	case "images":
		return filterMemes(memes, func(meme accessor.Meme) bool { return strings.HasPrefix(meme.ContentType, "image/") })
	case "mp3s":
		return filterMemes(memes, func(meme accessor.Meme) bool {
			return strings.HasPrefix(meme.ContentType, "audio/") || strings.HasSuffix(strings.ToLower(meme.OriginalName), ".mp3")
		})
	case "untagged":
		return filterMemes(memes, func(meme accessor.Meme) bool { return len(meme.Tags) == 0 })
	case "files":
		return filterMemes(memes, func(meme accessor.Meme) bool {
			return !strings.HasPrefix(meme.ContentType, "image/") &&
				!strings.HasPrefix(meme.ContentType, "video/") &&
				!strings.HasPrefix(meme.ContentType, "audio/") &&
				!strings.HasSuffix(strings.ToLower(meme.OriginalName), ".mp3")
		})
	default:
		return memes
	}
}

func filterMemes(memes []accessor.Meme, keep func(accessor.Meme) bool) []accessor.Meme {
	filtered := make([]accessor.Meme, 0, len(memes))
	for _, meme := range memes {
		if keep(meme) {
			filtered = append(filtered, meme)
		}
	}
	return filtered
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

// StoredMemeFiles lists the on-disk file names of every meme row, including
// hidden and pending-delete ones. It errors when the store cannot enumerate
// them, so a caller deciding what is safe to delete fails closed.
func (m *MemeManager) StoredMemeFiles() ([]accessor.StoredFile, error) {
	store, ok := m.store.(accessor.StoredFileStore)
	if !ok {
		return nil, errors.New("meme store cannot list stored files")
	}
	return store.AllStoredFiles()
}

// CountAuditActionsByActor counts audit events with the given action per actor.
// Stores without an audit log yield an empty map, not an error.
func (m *MemeManager) CountAuditActionsByActor(action string) (map[string]int, error) {
	store, ok := m.store.(accessor.AuditActionCountStore)
	if !ok {
		return map[string]int{}, nil
	}
	return store.CountAuditActionsByActor(action)
}

// CountFavoritesByUser counts favorites per user, or an empty map when the
// store cannot report it.
func (m *MemeManager) CountFavoritesByUser() (map[string]int, error) {
	store, ok := m.store.(accessor.FavoriteCountStore)
	if !ok {
		return map[string]int{}, nil
	}
	return store.CountFavoritesByUser()
}
