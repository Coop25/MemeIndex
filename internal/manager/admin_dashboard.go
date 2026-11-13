package manager

import (
	"log"
	"slices"
	"strings"
	"time"

	"memeindex/internal/accessor"
)

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

// AdminDashboard returns the admin overview stats, memoized for AdminReadCacheTTL
// so repeated loads (and the merge-then-refresh burst) do not each trigger a full
// store scan. Callers receive an independent deep copy.
func (m *MemeManager) AdminDashboard() AdminDashboardStats {
	m.adminReadMu.Lock()
	if m.adminDashboardCache != nil && time.Since(m.adminDashboardCachedAt) < AdminReadCacheTTL {
		cached := cloneAdminDashboardStats(*m.adminDashboardCache)
		m.adminReadMu.Unlock()
		return cached
	}
	m.adminReadMu.Unlock()

	stats := m.computeAdminDashboard()

	m.adminReadMu.Lock()
	stored := cloneAdminDashboardStats(stats)
	m.adminDashboardCache = &stored
	m.adminDashboardCachedAt = time.Now()
	m.adminReadMu.Unlock()
	return stats
}

func (m *MemeManager) computeAdminDashboard() AdminDashboardStats {
	memes := m.adminDashboardMemes()
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

// adminDashboardMemes returns every visible meme for the admin overview scan,
// using the lean SQL projection (no per-row favourites subquery) where the store
// offers it and falling back to a full List() otherwise. The admin favourites
// total is sourced separately from AdminAnalyticsStore, so the dropped per-row
// flag does not change the rendered dashboard.
func (m *MemeManager) adminDashboardMemes() []accessor.Meme {
	if ads, ok := m.store.(accessor.AdminDashboardMemeStore); ok {
		if memes, err := ads.AdminDashboardMemes(); err == nil {
			return memes
		} else {
			log.Printf("admin dashboard: lean meme scan failed, falling back to List: %v", err)
		}
	}
	return m.store.List("", "", false, "")
}

// tagUsageCounts returns tag name -> number of visible memes carrying it,
// resolved in SQL where the store supports it and by a full scan otherwise.
func (m *MemeManager) tagUsageCounts() map[string]int {
	if tu, ok := m.store.(accessor.TagUsageStore); ok {
		if counts, err := tu.TagUsageCounts(); err == nil {
			return counts
		} else {
			log.Printf("tag usage counts: SQL failed, falling back to scan: %v", err)
		}
	}

	counts := map[string]int{}
	for _, meme := range m.store.List("", "", false, "") {
		for _, tag := range meme.Tags {
			counts[tag] += 1
		}
	}
	return counts
}

// invalidateAdminReadCache drops the memoized admin dashboard and marks the
// tag-hygiene snapshot stale so it is refreshed. Call after any change that
// alters their inputs (tag merges, restore). The dashboard is cheap to rebuild
// and is dropped outright; the tag-hygiene recompute is expensive, so with the
// background worker running it is only nudged (the stale snapshot keeps serving
// until the worker swaps in a fresh one).
func (m *MemeManager) invalidateAdminReadCache() {
	m.adminReadMu.Lock()
	m.adminDashboardCache = nil
	m.tagHygieneDirty = true
	workerRunning := m.tagHygieneRefresh != nil
	if !workerRunning {
		m.tagHygieneCache = nil
	}
	m.adminReadMu.Unlock()
	if workerRunning {
		m.signalTagHygieneRefresh()
	}
}

func cloneAdminDashboardStats(in AdminDashboardStats) AdminDashboardStats {
	out := in
	out.UploadSeries = append([]AdminDashboardDayStat(nil), in.UploadSeries...)
	out.MetricSeries = append([]AdminDashboardMetricStat(nil), in.MetricSeries...)
	out.TopTags = append([]AdminDashboardTagStat(nil), in.TopTags...)
	out.RecentMemes = make([]AdminDashboardRecentMeme, len(in.RecentMemes))
	for i, meme := range in.RecentMemes {
		meme.Tags = append([]string(nil), in.RecentMemes[i].Tags...)
		out.RecentMemes[i] = meme
	}
	return out
}
