package client

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"memeindex/internal/manager"
)

// adminAudiobookDayStat is the cumulative audiobook count/bytes as of the end
// of one UTC day.
type adminAudiobookDayStat struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
	Bytes int64  `json:"bytes"`
}

// adminAudiobookStats is the audiobook side of the admin dashboard.
type adminAudiobookStats struct {
	Count               int                     `json:"count"`
	AddedLast30Days     int                     `json:"added_last_30_days"`
	AddedPrevious30Days int                     `json:"added_previous_30_days"`
	DurationSeconds     float64                 `json:"duration_seconds"`
	MediaBytes          int64                   `json:"media_bytes"`
	CoverBytes          int64                   `json:"cover_bytes"`
	UploadTempBytes     int64                   `json:"upload_temp_bytes"`
	TotalBytes          int64                   `json:"total_bytes"`
	BytesLast30Days     int64                   `json:"bytes_last_30_days"`
	BytesPrevious30Days int64                   `json:"bytes_previous_30_days"`
	Series              []adminAudiobookDayStat `json:"series"`
}

const adminAudiobookSeriesDays = 30

// AdminStats aggregates audiobook counts and storage for the admin dashboard.
// The series mirrors the manager's MetricSeries window: 30 UTC days, oldest
// first, the last being now's date.
func (s *postgresAudiobookStore) AdminStats(ctx context.Context, now time.Time) (adminAudiobookStats, error) {
	now = now.UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	windowStart := today.AddDate(0, 0, -(adminAudiobookSeriesDays - 1))
	last30 := now.AddDate(0, 0, -30)
	prev30 := now.AddDate(0, 0, -60)

	var stats adminAudiobookStats
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*),
			COALESCE(SUM(duration_seconds), 0),
			COALESCE(SUM(size_bytes), 0),
			COUNT(*) FILTER (WHERE created_at >= $1),
			COALESCE(SUM(size_bytes) FILTER (WHERE created_at >= $1), 0),
			COUNT(*) FILTER (WHERE created_at >= $2 AND created_at < $1),
			COALESCE(SUM(size_bytes) FILTER (WHERE created_at >= $2 AND created_at < $1), 0)
		FROM audiobooks
	`, last30, prev30).Scan(&stats.Count, &stats.DurationSeconds, &stats.MediaBytes,
		&stats.AddedLast30Days, &stats.BytesLast30Days, &stats.AddedPrevious30Days, &stats.BytesPrevious30Days)
	if err != nil {
		return adminAudiobookStats{}, fmt.Errorf("aggregate audiobooks: %w", err)
	}

	if err := s.pool.QueryRow(ctx, `SELECT COALESCE(SUM(received_bytes), 0) FROM audiobook_uploads`).Scan(&stats.UploadTempBytes); err != nil {
		return adminAudiobookStats{}, fmt.Errorf("sum audiobook uploads: %w", err)
	}

	coverRows, err := s.pool.Query(ctx, `SELECT cover_path FROM audiobooks WHERE cover_path <> ''`)
	if err != nil {
		return adminAudiobookStats{}, fmt.Errorf("list audiobook covers: %w", err)
	}
	for coverRows.Next() {
		var path string
		if err := coverRows.Scan(&path); err != nil {
			coverRows.Close()
			return adminAudiobookStats{}, fmt.Errorf("scan audiobook cover: %w", err)
		}
		if info, statErr := os.Stat(path); statErr == nil && !info.IsDir() {
			stats.CoverBytes += info.Size()
		}
	}
	coverRows.Close()
	if err := coverRows.Err(); err != nil {
		return adminAudiobookStats{}, fmt.Errorf("list audiobook covers: %w", err)
	}
	stats.TotalBytes = stats.MediaBytes + stats.CoverBytes + stats.UploadTempBytes

	// Daily additions inside the window; everything older is the baseline.
	dayRows, err := s.pool.Query(ctx, `
		SELECT to_char(created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD'), COUNT(*), COALESCE(SUM(size_bytes), 0)
		FROM audiobooks
		WHERE created_at >= $1
		GROUP BY 1
	`, windowStart)
	if err != nil {
		return adminAudiobookStats{}, fmt.Errorf("aggregate audiobook days: %w", err)
	}
	type dayAdd struct {
		count int
		bytes int64
	}
	adds := map[string]dayAdd{}
	windowCount, windowBytes := 0, int64(0)
	for dayRows.Next() {
		var date string
		var add dayAdd
		if err := dayRows.Scan(&date, &add.count, &add.bytes); err != nil {
			dayRows.Close()
			return adminAudiobookStats{}, fmt.Errorf("scan audiobook day: %w", err)
		}
		adds[date] = add
		windowCount += add.count
		windowBytes += add.bytes
	}
	dayRows.Close()
	if err := dayRows.Err(); err != nil {
		return adminAudiobookStats{}, fmt.Errorf("aggregate audiobook days: %w", err)
	}

	runningCount := stats.Count - windowCount
	runningBytes := stats.MediaBytes - windowBytes
	stats.Series = make([]adminAudiobookDayStat, adminAudiobookSeriesDays)
	for index := range stats.Series {
		date := today.AddDate(0, 0, index-(adminAudiobookSeriesDays-1)).Format("2006-01-02")
		add := adds[date]
		runningCount += add.count
		runningBytes += add.bytes
		stats.Series[index] = adminAudiobookDayStat{Date: date, Count: runningCount, Bytes: runningBytes}
	}
	return stats, nil
}

// audiobookAdminStatsCache memoizes AdminStats for manager.AdminReadCacheTTL.
type audiobookAdminStatsCache struct {
	mu       sync.Mutex
	stats    adminAudiobookStats
	cachedAt time.Time
	valid    bool
}

// cachedAudiobookAdminStats returns the audiobook admin aggregate, or nil when
// audiobooks are not configured.
func (s *Server) cachedAudiobookAdminStats(ctx context.Context) (*adminAudiobookStats, error) {
	if s.audiobooks == nil {
		return nil, nil
	}
	c := &s.audiobookAdminStats
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.valid && time.Since(c.cachedAt) < manager.AdminReadCacheTTL {
		out := c.stats
		out.Series = append([]adminAudiobookDayStat(nil), c.stats.Series...)
		return &out, nil
	}
	stats, err := s.audiobooks.AdminStats(ctx, time.Now())
	if err != nil {
		return nil, err
	}
	c.stats, c.cachedAt, c.valid = stats, time.Now(), true
	out := stats
	out.Series = append([]adminAudiobookDayStat(nil), stats.Series...)
	return &out, nil
}
