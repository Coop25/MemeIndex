package manager

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"net/textproto"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"memeindex/internal/accessor"
	"memeindex/internal/tagsuggest"
)

var errAutoSuggestExhausted = errors.New("tag suggestion attempts exhausted")

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
	suggestionWorkerStart sync.Once
	suggestionWorkerReady bool
	suggestionWorkerState string
	suggestionCurrentID   string
	suggestionLastError   string
	suggestionLastSuccess time.Time
}

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
		suggestionWorkerState: func() string {
			if tagSuggester == nil || !tagSuggester.Enabled() {
				return "disabled"
			}
			return "starting"
		}(),
	}
	manager.suggestionQueueCond = sync.NewCond(&manager.suggestionQueueMu)
	return manager
}

func (m *MemeManager) ListMemes(userID, query string, favoritesOnly bool, tag string, view string, offset int, limit int) MemeListResult {
	return m.ListMemesSorted(userID, query, favoritesOnly, tag, view, "newest", offset, limit)
}

func (m *MemeManager) ListMemesSorted(userID, query string, favoritesOnly bool, tag string, view string, sortBy string, offset int, limit int) MemeListResult {
	source := m.store.List(strings.TrimSpace(userID), strings.TrimSpace(query), false, strings.TrimSpace(tag))
	counts := buildMemeCounts(source)
	visible := filterMemesByView(source, strings.TrimSpace(view))
	if favoritesOnly {
		visible = filterMemesByView(visible, "favorites")
	}
	sortMemes(visible, sortBy)

	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 72
	}

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

func (m *MemeManager) Dashboard(userID string) VaultDashboard {
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
			if len(dashboard.FavoriteItems) < 6 {
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
	dashboard.RecentItems = append(dashboard.RecentItems, items[:limit]...)
	for _, index := range rand.Perm(len(items))[:limit] {
		dashboard.RandomItems = append(dashboard.RandomItems, items[index])
	}
	dashboard.TagCount = len(tagCounts)
	dashboard.TopTags = topTagStats(tagCounts, 5)
	return dashboard
}

func (m *MemeManager) PopularTags(userID string, limit int) []VaultTagStat {
	if limit <= 0 {
		limit = 10
	}
	limit = min(limit, 50)

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
	return m.store.Create(accessor.CreateInput{
		File:      file,
		Header:    header,
		Filename:  filename,
		Tags:      normalizeTags(tags),
		Notes:     strings.TrimSpace(notes),
		SourceURL: strings.TrimSpace(sourceURL),
		Actor:     actor,
	})
}

func (m *MemeManager) UpdateMeme(userID, id string, update accessor.MemeUpdate) (accessor.Meme, error) {
	update.Tags = normalizeTags(update.Tags)
	update.Notes = strings.TrimSpace(update.Notes)
	return m.store.Update(strings.TrimSpace(userID), strings.TrimSpace(id), update)
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

func (m *MemeManager) GetOrCreateMemeShare(memeID, userID string, now time.Time) (accessor.MemeShareState, error) {
	store, ok := m.store.(accessor.MemeShareStore)
	if !ok {
		return accessor.MemeShareState{}, errors.New("meme sharing is unavailable")
	}
	return store.GetOrCreateMemeShare(strings.TrimSpace(memeID), strings.TrimSpace(userID), now.UTC(), now.UTC().Add(30*24*time.Hour))
}

func (m *MemeManager) GetMemeShare(memeID string) (accessor.MemeShareState, accessor.Meme, error) {
	store, ok := m.store.(accessor.MemeShareStore)
	if !ok {
		return accessor.MemeShareState{}, accessor.Meme{}, errors.New("meme sharing is unavailable")
	}
	share, err := store.GetMemeShareState(strings.TrimSpace(memeID))
	if err != nil {
		return accessor.MemeShareState{}, accessor.Meme{}, err
	}
	meme, err := m.store.GetByID("", share.MemeID)
	return share, meme, err
}

type ActiveMemeShare struct {
	Meme  accessor.Meme           `json:"meme"`
	Share accessor.MemeShareState `json:"share"`
}

func (m *MemeManager) ListActiveMemeShares(now time.Time) ([]ActiveMemeShare, error) {
	store, ok := m.store.(accessor.MemeShareStore)
	if !ok {
		return nil, errors.New("meme sharing is unavailable")
	}
	states, err := store.ListActiveMemeShares(now.UTC())
	if err != nil {
		return nil, err
	}
	result := make([]ActiveMemeShare, 0, len(states))
	for _, state := range states {
		meme, err := m.store.GetByID("", state.MemeID)
		if err == nil {
			result = append(result, ActiveMemeShare{Meme: meme, Share: state})
		}
	}
	return result, nil
}

func (m *MemeManager) RevokeMemeShare(memeID string) error {
	store, ok := m.store.(accessor.MemeShareStore)
	if !ok {
		return errors.New("meme sharing is unavailable")
	}
	return store.RevokeMemeShare(strings.TrimSpace(memeID))
}

func (m *MemeManager) GetAdminMeme(id string) (accessor.Meme, error) {
	store, ok := m.store.(accessor.AdminMemeStore)
	if ok {
		return store.GetAnyByID(strings.TrimSpace(id))
	}
	return m.store.GetByID("", strings.TrimSpace(id))
}

func (m *MemeManager) DeleteMeme(id string, actor accessor.AuditActor) (accessor.DeleteResult, error) {
	return m.store.Delete(accessor.DeleteInput{
		ID:    strings.TrimSpace(id),
		Actor: actor,
	})
}

func (m *MemeManager) SuggestTags(prefix string, limit int) []string {
	return m.store.SuggestTags(strings.TrimSpace(prefix), limit)
}

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

type AdminDashboardStats struct {
	Counts                 MemeCounts                 `json:"counts"`
	TaggedMemes            int                        `json:"tagged_memes"`
	PendingSuggestionMemes int                        `json:"pending_suggestion_memes"`
	UniqueTags             int                        `json:"unique_tags"`
	TotalTagAssignments    int                        `json:"total_tag_assignments"`
	AverageTagsPerMeme     float64                    `json:"average_tags_per_meme"`
	TotalSizeBytes         int64                      `json:"total_size_bytes"`
	ImageSizeBytes         int64                      `json:"image_size_bytes"`
	VideoSizeBytes         int64                      `json:"video_size_bytes"`
	AudioSizeBytes         int64                      `json:"audio_size_bytes"`
	OtherSizeBytes         int64                      `json:"other_size_bytes"`
	UploadedLast24Hours    int                        `json:"uploaded_last_24_hours"`
	UploadedLast7Days      int                        `json:"uploaded_last_7_days"`
	UploadedLast30Days     int                        `json:"uploaded_last_30_days"`
	UploadedPrevious30Days int                        `json:"uploaded_previous_30_days"`
	BytesLast30Days        int64                      `json:"bytes_last_30_days"`
	BytesPrevious30Days    int64                      `json:"bytes_previous_30_days"`
	UploadSeries           []AdminDashboardDayStat    `json:"upload_series"`
	MetricSeries           []AdminDashboardMetricStat `json:"metric_series"`
	RecentMemes            []AdminDashboardRecentMeme `json:"recent_memes"`
	TopTags                []AdminDashboardTagStat    `json:"top_tags"`
}

type AdminDashboardDayStat struct {
	Date    string `json:"date"`
	Uploads int    `json:"uploads"`
	Bytes   int64  `json:"bytes"`
}

type AdminDashboardMetricStat struct {
	Date         string `json:"date"`
	Memes        int    `json:"memes"`
	Tags         int    `json:"tags"`
	Users        int    `json:"users"`
	StorageBytes int64  `json:"storage_bytes"`
	Favorites    int    `json:"favorites"`
}

type AdminDashboardRecentMeme struct {
	ID           string    `json:"id"`
	OriginalName string    `json:"original_name"`
	ContentType  string    `json:"content_type"`
	FilePath     string    `json:"file_path"`
	PreviewPath  string    `json:"preview_path"`
	SizeBytes    int64     `json:"size_bytes"`
	Tags         []string  `json:"tags"`
	CreatedAt    time.Time `json:"created_at"`
	TagCount     int       `json:"tag_count"`
}

type AdminDashboardTagStat struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

type TagHygieneReport struct {
	Tags  []TagHygieneTag  `json:"tags"`
	Pairs []TagHygienePair `json:"pairs"`
}

type TagHygieneTag struct {
	Tag     string   `json:"tag"`
	Count   int      `json:"count"`
	Similar []string `json:"similar,omitempty"`
}

type TagHygienePair struct {
	Primary   string `json:"primary"`
	Candidate string `json:"candidate"`
}

type TagMergeResult struct {
	SourceTag     string `json:"source_tag"`
	TargetTag     string `json:"target_tag"`
	AffectedMemes int    `json:"affected_memes"`
}

func (m *MemeManager) GetStoredMemeTagSuggestions(id string) (MemeTagSuggestionResult, error) {
	suggestionStore, ok := m.store.(accessor.SuggestedTagStore)
	if !ok {
		return MemeTagSuggestionResult{}, tagsuggest.ErrDisabled
	}

	tags, err := suggestionStore.ListSuggestedTags(strings.TrimSpace(id))
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

	meme, err := m.store.GetByID(strings.TrimSpace(userID), strings.TrimSpace(id))
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

	suggestionStore, ok := m.store.(accessor.SuggestedTagStore)
	if ok {
		if err := suggestionStore.ReplaceSuggestedTags(meme.ID, output.Tags); err != nil {
			return MemeTagSuggestionResult{}, err
		}
		if err := suggestionStore.SetAutoSuggestDisabled(meme.ID, false); err != nil {
			return MemeTagSuggestionResult{}, err
		}
	}

	return output, nil
}

func (m *MemeManager) QueueMemeTagSuggestions(id string) {
	if m.tagSuggester == nil || !m.tagSuggester.Enabled() {
		return
	}
	memeID := strings.TrimSpace(id)
	if memeID == "" {
		return
	}

	meme, err := m.store.GetByID("", memeID)
	if err != nil {
		return
	}
	if !shouldQueueTagSuggestionsForMeme(meme) {
		return
	}

	m.enqueueTagSuggestion(memeID)
}

func (m *MemeManager) ResetTagSuggestionsAndRequeueUntagged() (ResetTagSuggestionQueueResult, error) {
	suggestionStore, ok := m.store.(accessor.SuggestedTagStore)
	if !ok {
		return ResetTagSuggestionQueueResult{}, tagsuggest.ErrDisabled
	}

	memes := m.store.List("", "", false, "")
	result := ResetTagSuggestionQueueResult{}
	for _, meme := range memes {
		if len(meme.SuggestedTags) > 0 {
			if err := suggestionStore.ReplaceSuggestedTags(meme.ID, nil); err != nil {
				return ResetTagSuggestionQueueResult{}, err
			}
			result.ClearedSuggestions += 1
		}
		if meme.AutoSuggestDisabled {
			if err := suggestionStore.SetAutoSuggestDisabled(meme.ID, false); err != nil {
				return ResetTagSuggestionQueueResult{}, err
			}
			result.ClearedExhausted += 1
		}

		if len(meme.Tags) == 0 && m.enqueueTagSuggestion(meme.ID) {
			result.QueuedUntagged += 1
		}
	}

	return result, nil
}

func (m *MemeManager) AdminDashboard() AdminDashboardStats {
	memes := m.store.List("", "", false, "")
	stats := AdminDashboardStats{
		Counts:       buildMemeCounts(memes),
		UploadSeries: make([]AdminDashboardDayStat, 30),
		MetricSeries: make([]AdminDashboardMetricStat, 30),
		RecentMemes:  make([]AdminDashboardRecentMeme, 0, min(6, len(memes))),
		TopTags:      []AdminDashboardTagStat{},
	}

	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	seriesIndex := map[string]int{}
	for index := range stats.UploadSeries {
		date := today.AddDate(0, 0, index-29).Format("2006-01-02")
		seriesIndex[date] = index
		stats.UploadSeries[index] = AdminDashboardDayStat{Date: date}
		stats.MetricSeries[index] = AdminDashboardMetricStat{Date: date}
	}
	tagCounts := map[string]int{}
	tagFirstSeen := map[string]time.Time{}
	for _, meme := range memes {
		stats.TotalSizeBytes += meme.SizeBytes
		if len(meme.Tags) > 0 {
			stats.TaggedMemes += 1
		}
		if len(meme.SuggestedTags) > 0 {
			stats.PendingSuggestionMemes += 1
		}
		stats.TotalTagAssignments += len(meme.Tags)
		for _, tag := range meme.Tags {
			tagCounts[tag] += 1
			if firstSeen, ok := tagFirstSeen[tag]; !ok || meme.CreatedAt.Before(firstSeen) {
				tagFirstSeen[tag] = meme.CreatedAt
			}
		}

		age := now.Sub(meme.CreatedAt)
		if age <= 24*time.Hour {
			stats.UploadedLast24Hours += 1
		}
		if age <= 7*24*time.Hour {
			stats.UploadedLast7Days += 1
		}
		if age <= 30*24*time.Hour {
			stats.UploadedLast30Days += 1
			stats.BytesLast30Days += meme.SizeBytes
		} else if age <= 60*24*time.Hour {
			stats.UploadedPrevious30Days += 1
			stats.BytesPrevious30Days += meme.SizeBytes
		}
		if index, ok := seriesIndex[meme.CreatedAt.UTC().Format("2006-01-02")]; ok {
			stats.UploadSeries[index].Uploads += 1
			stats.UploadSeries[index].Bytes += meme.SizeBytes
			stats.MetricSeries[index].Memes += 1
			stats.MetricSeries[index].StorageBytes += meme.SizeBytes
		}

		switch {
		case strings.HasPrefix(meme.ContentType, "image/"):
			stats.ImageSizeBytes += meme.SizeBytes
		case strings.HasPrefix(meme.ContentType, "video/"):
			stats.VideoSizeBytes += meme.SizeBytes
		case strings.HasPrefix(meme.ContentType, "audio/") || strings.HasSuffix(strings.ToLower(meme.OriginalName), ".mp3"):
			stats.AudioSizeBytes += meme.SizeBytes
		default:
			stats.OtherSizeBytes += meme.SizeBytes
		}
	}

	if len(memes) > 0 {
		stats.AverageTagsPerMeme = float64(stats.TotalTagAssignments) / float64(len(memes))
	}
	stats.UniqueTags = len(tagCounts)
	for _, firstSeen := range tagFirstSeen {
		if index, ok := seriesIndex[firstSeen.UTC().Format("2006-01-02")]; ok {
			stats.MetricSeries[index].Tags += 1
		}
	}

	favoriteDeltas := make([]int, len(stats.MetricSeries))
	if analytics, ok := m.store.(accessor.AdminAnalyticsStore); ok {
		if totalFavorites, err := analytics.TotalFavoriteAssignments(); err == nil {
			stats.Counts.Favorites = totalFavorites
		}
		if activity, err := analytics.FavoriteActivitySince(today.AddDate(0, 0, -29)); err == nil {
			for _, day := range activity {
				if index, ok := seriesIndex[day.Date.UTC().Format("2006-01-02")]; ok {
					favoriteDeltas[index] += day.Added - day.Removed
				}
			}
		}
	}

	recentMemes := 0
	recentTags := 0
	var recentStorage int64
	recentFavoriteDelta := 0
	for index, point := range stats.MetricSeries {
		recentMemes += point.Memes
		recentTags += point.Tags
		recentStorage += point.StorageBytes
		recentFavoriteDelta += favoriteDeltas[index]
	}
	runningMemes := max(0, stats.Counts.Total-recentMemes)
	runningTags := max(0, stats.UniqueTags-recentTags)
	runningStorage := max(int64(0), stats.TotalSizeBytes-recentStorage)
	runningFavorites := max(0, stats.Counts.Favorites-recentFavoriteDelta)
	for index := range stats.MetricSeries {
		runningMemes += stats.MetricSeries[index].Memes
		runningTags += stats.MetricSeries[index].Tags
		runningStorage += stats.MetricSeries[index].StorageBytes
		runningFavorites += favoriteDeltas[index]
		stats.MetricSeries[index].Memes = runningMemes
		stats.MetricSeries[index].Tags = runningTags
		stats.MetricSeries[index].StorageBytes = runningStorage
		stats.MetricSeries[index].Favorites = max(0, runningFavorites)
	}

	recentLimit := min(6, len(memes))
	for _, meme := range memes[:recentLimit] {
		stats.RecentMemes = append(stats.RecentMemes, AdminDashboardRecentMeme{
			ID:           meme.ID,
			OriginalName: meme.OriginalName,
			ContentType:  meme.ContentType,
			FilePath:     meme.FilePath,
			PreviewPath:  meme.PreviewPath,
			SizeBytes:    meme.SizeBytes,
			Tags:         append([]string(nil), meme.Tags...),
			CreatedAt:    meme.CreatedAt,
			TagCount:     len(meme.Tags),
		})
	}

	if len(tagCounts) > 0 {
		tagStats := make([]AdminDashboardTagStat, 0, len(tagCounts))
		for tag, count := range tagCounts {
			tagStats = append(tagStats, AdminDashboardTagStat{Tag: tag, Count: count})
		}
		slices.SortFunc(tagStats, func(left, right AdminDashboardTagStat) int {
			if left.Count != right.Count {
				return right.Count - left.Count
			}
			return strings.Compare(left.Tag, right.Tag)
		})
		if len(tagStats) > 12 {
			tagStats = tagStats[:12]
		}
		stats.TopTags = tagStats
	}

	return stats
}

func (m *MemeManager) TagHygieneReport() TagHygieneReport {
	memes := m.store.List("", "", false, "")
	tagCounts := map[string]int{}
	for _, meme := range memes {
		for _, tag := range meme.Tags {
			tagCounts[tag] += 1
		}
	}

	tagNames := make([]string, 0, len(tagCounts))
	for tag := range tagCounts {
		tagNames = append(tagNames, tag)
	}
	slices.Sort(tagNames)

	similarMap := map[string][]string{}
	pairs := make([]TagHygienePair, 0)
	for index, left := range tagNames {
		for _, right := range tagNames[index+1:] {
			if !looksLikeTagVariant(left, right) {
				continue
			}
			primary := preferredCanonicalTag(left, right, tagCounts)
			candidate := right
			if primary == right {
				candidate = left
			}
			similarMap[primary] = append(similarMap[primary], candidate)
			pairs = append(pairs, TagHygienePair{
				Primary:   primary,
				Candidate: candidate,
			})
		}
	}

	items := make([]TagHygieneTag, 0, len(tagNames))
	for _, tag := range tagNames {
		items = append(items, TagHygieneTag{
			Tag:     tag,
			Count:   tagCounts[tag],
			Similar: normalizeTags(similarMap[tag]),
		})
	}
	slices.SortFunc(items, func(left, right TagHygieneTag) int {
		if len(left.Similar) != len(right.Similar) {
			return len(right.Similar) - len(left.Similar)
		}
		if left.Count != right.Count {
			return right.Count - left.Count
		}
		return strings.Compare(left.Tag, right.Tag)
	})

	slices.SortFunc(pairs, func(left, right TagHygienePair) int {
		if left.Primary != right.Primary {
			return strings.Compare(left.Primary, right.Primary)
		}
		return strings.Compare(left.Candidate, right.Candidate)
	})

	return TagHygieneReport{
		Tags:  items,
		Pairs: pairs,
	}
}

func (m *MemeManager) MergeTags(sourceTag string, targetTag string, actor accessor.AuditActor) (TagMergeResult, error) {
	sourceTag = strings.ToLower(strings.TrimSpace(sourceTag))
	targetTag = strings.ToLower(strings.TrimSpace(targetTag))
	if sourceTag == "" || targetTag == "" {
		return TagMergeResult{}, errors.New("source and target tags are required")
	}
	if sourceTag == targetTag {
		return TagMergeResult{
			SourceTag: sourceTag,
			TargetTag: targetTag,
		}, nil
	}

	memes := m.store.List("", "", false, "")
	result := TagMergeResult{
		SourceTag: sourceTag,
		TargetTag: targetTag,
	}
	for _, meme := range memes {
		if !containsTag(meme.Tags, sourceTag) {
			continue
		}
		nextTags := make([]string, 0, len(meme.Tags))
		for _, tag := range meme.Tags {
			normalized := strings.ToLower(strings.TrimSpace(tag))
			if normalized == sourceTag {
				nextTags = append(nextTags, targetTag)
				continue
			}
			nextTags = append(nextTags, normalized)
		}
		if _, err := m.store.Update("", meme.ID, accessor.MemeUpdate{
			Tags:     nextTags,
			Notes:    meme.Notes,
			Favorite: meme.Favorite,
			Actor:    actor,
		}); err != nil {
			return TagMergeResult{}, err
		}
		result.AffectedMemes += 1
	}

	return result, nil
}

func (m *MemeManager) TagSuggestionQueueStatus(reviewOffset int, reviewLimit int) TagSuggestionQueueStatus {
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

	if status.CurrentMemeID != "" {
		if meme, err := m.store.GetByID("", status.CurrentMemeID); err == nil {
			status.CurrentMemeName = meme.OriginalName
		}
	}

	if len(queuedIDs) > 0 {
		status.QueuedMemes = make([]QueuedTagSuggestionItem, 0, len(queuedIDs))
		for _, id := range queuedIDs {
			item := QueuedTagSuggestionItem{ID: id}
			if meme, err := m.store.GetByID("", id); err == nil {
				item.Name = meme.OriginalName
			}
			status.QueuedMemes = append(status.QueuedMemes, item)
		}
	}

	memes := m.store.List("", "", false, "")
	for _, meme := range memes {
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

	return status
}

func (m *MemeManager) StartTagSuggestionWorker() {
	if m.tagSuggester == nil || !m.tagSuggester.Enabled() {
		return
	}

	m.suggestionWorkerStart.Do(func() {
		go m.runTagSuggestionWorker()
	})
}

func (m *MemeManager) SeedTagSuggestionQueue() int {
	if m.tagSuggester == nil || !m.tagSuggester.Enabled() {
		return 0
	}

	memes := m.store.List("", "", false, "")
	queued := 0
	for _, meme := range memes {
		if !shouldQueueTagSuggestionsForMeme(meme) {
			continue
		}
		if m.enqueueTagSuggestion(meme.ID) {
			queued += 1
		}
	}
	return queued
}

// ReloadAfterRestore refreshes the small pieces of state cached in memory after
// an administrator replaces the persistent database with a portable backup.
func (m *MemeManager) ReloadAfterRestore() error {
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
	return nil
}

func (m *MemeManager) runTagSuggestionWorker() {
	log.Printf("tag suggestion worker: waiting for Ollama model %q", m.tagSuggester.Model())
	m.setTagSuggestionWorkerState("waiting_for_ollama", false, "", "")
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err := m.tagSuggester.WaitUntilReady(ctx)
		cancel()
		if err == nil {
			break
		}
		m.setTagSuggestionWorkerState("waiting_for_ollama", false, "", err.Error())
		log.Printf("tag suggestion worker: Ollama not ready yet: %v", err)
		time.Sleep(3 * time.Second)
	}
	m.setTagSuggestionWorkerState("idle", true, "", "")
	log.Printf("tag suggestion worker: Ollama is ready")

	for {
		memeID := m.dequeueTagSuggestion()
		m.setTagSuggestionWorkerState("processing", true, memeID, "")
		timeout := 7 * time.Minute
		if m.tagSuggester != nil {
			timeout = m.tagSuggester.Timeout() + (2 * time.Minute)
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		_, err := m.RefreshMemeTagSuggestions(ctx, "", memeID)
		cancel()
		if err == nil {
			m.finishTagSuggestion(memeID)
			m.recordTagSuggestionSuccess()
			continue
		}

		switch {
		case errors.Is(err, errAutoSuggestExhausted):
			log.Printf("tag suggestion worker: exhausted retries for meme %s; marking auto-suggest disabled", memeID)
			m.finishTagSuggestion(memeID)
			m.setTagSuggestionWorkerState("idle", true, "", err.Error())
		case errors.Is(err, os.ErrNotExist), errors.Is(err, tagsuggest.ErrUnsupported):
			log.Printf("tag suggestion worker: dropping meme %s from queue: %v", memeID, err)
			m.finishTagSuggestion(memeID)
			m.setTagSuggestionWorkerState("idle", true, "", err.Error())
		case errors.Is(err, tagsuggest.ErrUnavailable):
			log.Printf("tag suggestion worker: retrying meme %s later after Ollama error: %v", memeID, err)
			m.requeueTagSuggestion(memeID)
			m.setTagSuggestionWorkerState("waiting_for_ollama", false, "", err.Error())
			time.Sleep(3 * time.Second)
		default:
			log.Printf("tag suggestion worker: unexpected error for meme %s, retrying later: %v", memeID, err)
			m.requeueTagSuggestion(memeID)
			m.setTagSuggestionWorkerState("retrying", true, "", err.Error())
			time.Sleep(3 * time.Second)
		}
	}
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

func (m *MemeManager) dequeueTagSuggestion() string {
	m.suggestionQueueMu.Lock()
	defer m.suggestionQueueMu.Unlock()

	for len(m.suggestionQueue) == 0 {
		m.suggestionQueueCond.Wait()
	}

	id := m.suggestionQueue[0]
	m.suggestionQueue = m.suggestionQueue[1:]
	return id
}

func (m *MemeManager) finishTagSuggestion(id string) {
	m.suggestionQueueMu.Lock()
	defer m.suggestionQueueMu.Unlock()
	delete(m.queuedSuggestionIDs, strings.TrimSpace(id))
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

func (m *MemeManager) DismissMemeTagSuggestion(userID string, id string, tag string) (accessor.Meme, error) {
	suggestionStore, ok := m.store.(accessor.SuggestedTagStore)
	if !ok {
		return accessor.Meme{}, tagsuggest.ErrDisabled
	}

	meme, err := m.store.GetByID(strings.TrimSpace(userID), strings.TrimSpace(id))
	if err != nil {
		return accessor.Meme{}, err
	}

	if err := suggestionStore.ReplaceSuggestedTags(meme.ID, removeTagValue(meme.SuggestedTags, tag)); err != nil {
		return accessor.Meme{}, err
	}

	return m.store.GetByID(strings.TrimSpace(userID), meme.ID)
}

func (m *MemeManager) DismissAllMemeTagSuggestions(userID string, id string) (accessor.Meme, error) {
	suggestionStore, ok := m.store.(accessor.SuggestedTagStore)
	if !ok {
		return accessor.Meme{}, tagsuggest.ErrDisabled
	}

	meme, err := m.store.GetByID(strings.TrimSpace(userID), strings.TrimSpace(id))
	if err != nil {
		return accessor.Meme{}, err
	}
	if err := suggestionStore.ReplaceSuggestedTags(meme.ID, nil); err != nil {
		return accessor.Meme{}, err
	}

	return m.store.GetByID(strings.TrimSpace(userID), meme.ID)
}

func (m *MemeManager) ApplyMemeTagSuggestion(userID string, id string, tag string, actor accessor.AuditActor) (accessor.Meme, error) {
	meme, err := m.store.GetByID(strings.TrimSpace(userID), strings.TrimSpace(id))
	if err != nil {
		return accessor.Meme{}, err
	}

	normalizedTag := strings.ToLower(strings.TrimSpace(tag))
	if normalizedTag == "" {
		return meme, nil
	}

	nextTags := append([]string(nil), meme.Tags...)
	if !containsTag(nextTags, normalizedTag) {
		nextTags = append(nextTags, normalizedTag)
	}

	if _, err := m.store.Update(strings.TrimSpace(userID), meme.ID, accessor.MemeUpdate{
		Tags:     nextTags,
		Notes:    meme.Notes,
		Favorite: meme.Favorite,
		Actor:    actor,
	}); err != nil {
		return accessor.Meme{}, err
	}

	suggestionStore, ok := m.store.(accessor.SuggestedTagStore)
	if ok {
		if err := suggestionStore.ReplaceSuggestedTags(meme.ID, removeTagValue(meme.SuggestedTags, normalizedTag)); err != nil {
			return accessor.Meme{}, err
		}
	}

	return m.store.GetByID(strings.TrimSpace(userID), meme.ID)
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

func (m *MemeManager) ListMemeAudit(id string, limit int) ([]accessor.MemeAuditEntry, error) {
	store, ok := m.store.(accessor.AuditLogStore)
	if !ok {
		return nil, nil
	}
	return store.ListMemeAudit(strings.TrimSpace(id), limit)
}

func (m *MemeManager) ListAuditFeed(offset int, limit int) (accessor.PagedAuditFeed, error) {
	store, ok := m.store.(accessor.AuditLogStore)
	if !ok {
		return accessor.PagedAuditFeed{}, nil
	}
	return store.ListAuditFeed(offset, limit)
}

func (m *MemeManager) ListPendingDeletes(offset int, limit int) (accessor.PagedPendingDeletes, error) {
	store, ok := m.store.(accessor.AuditLogStore)
	if !ok {
		return accessor.PagedPendingDeletes{}, nil
	}
	return store.ListPendingDeletes(offset, limit)
}

func (m *MemeManager) ApprovePendingDelete(id string, actor accessor.AuditActor) error {
	store, ok := m.store.(accessor.AuditLogStore)
	if !ok {
		return nil
	}
	return store.ApprovePendingDelete(strings.TrimSpace(id), actor)
}

func (m *MemeManager) RejectPendingDelete(id string, actor accessor.AuditActor) error {
	store, ok := m.store.(accessor.AuditLogStore)
	if !ok {
		return nil
	}
	return store.RejectPendingDelete(strings.TrimSpace(id), actor)
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
		request.KnownTags = m.store.SuggestTags("", m.knownTagHint)

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
			if err := suggestionStore.SetAutoSuggestDisabled(meme.ID, true); err != nil {
				return tagsuggest.Result{}, err
			}
		}
		return tagsuggest.Result{}, fmt.Errorf("%w: %v", errAutoSuggestExhausted, lastInvalidResponse)
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

func shouldQueueTagSuggestionsForMeme(meme accessor.Meme) bool {
	return len(meme.Tags) == 0 && len(meme.SuggestedTags) == 0 && !meme.AutoSuggestDisabled
}

func looksLikeTagVariant(left string, right string) bool {
	left = strings.ToLower(strings.TrimSpace(left))
	right = strings.ToLower(strings.TrimSpace(right))
	if left == "" || right == "" || left == right {
		return false
	}

	leftCanonical := compactTagKey(left)
	rightCanonical := compactTagKey(right)
	if leftCanonical == rightCanonical {
		return true
	}

	if absInt(len(leftCanonical)-len(rightCanonical)) > 2 {
		return false
	}
	if min(len(leftCanonical), len(rightCanonical)) < 4 {
		return false
	}

	return levenshteinDistance(leftCanonical, rightCanonical) <= 2
}

func preferredCanonicalTag(left string, right string, counts map[string]int) string {
	left = strings.ToLower(strings.TrimSpace(left))
	right = strings.ToLower(strings.TrimSpace(right))
	if counts[left] != counts[right] {
		if counts[left] > counts[right] {
			return left
		}
		return right
	}
	if compactTagKey(left) == compactTagKey(right) {
		if len(left) < len(right) {
			return left
		}
		if len(right) < len(left) {
			return right
		}
	}
	if strings.Compare(left, right) <= 0 {
		return left
	}
	return right
}

func compactTagKey(tag string) string {
	tag = strings.ToLower(strings.TrimSpace(tag))
	replacer := strings.NewReplacer(" ", "", "-", "", "_", "", ".", "", ",", "", "'", "", "\"", "")
	return replacer.Replace(tag)
}

func levenshteinDistance(left string, right string) int {
	if left == right {
		return 0
	}
	if left == "" {
		return len(right)
	}
	if right == "" {
		return len(left)
	}

	prev := make([]int, len(right)+1)
	curr := make([]int, len(right)+1)
	for column := 0; column <= len(right); column++ {
		prev[column] = column
	}

	for row := 1; row <= len(left); row++ {
		curr[0] = row
		for column := 1; column <= len(right); column++ {
			cost := 0
			if left[row-1] != right[column-1] {
				cost = 1
			}
			curr[column] = minInt(
				curr[column-1]+1,
				prev[column]+1,
				prev[column-1]+cost,
			)
		}
		copy(prev, curr)
	}

	return prev[len(right)]
}

func minInt(values ...int) int {
	best := values[0]
	for _, value := range values[1:] {
		if value < best {
			best = value
		}
	}
	return best
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
