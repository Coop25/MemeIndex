package audiobooks

import (
	"context"
	"fmt"
	"os"
	"time"
)

const adminAudiobookSeriesDays = 30

// AdminDayStat is the cumulative audiobook count/bytes as of the end
// of one UTC day.
type AdminDayStat struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
	Bytes int64  `json:"bytes"`
}

// AdminStatsReport is the audiobook side of the admin dashboard.
type AdminStatsReport struct {
	Count               int            `json:"count"`
	AddedLast30Days     int            `json:"added_last_30_days"`
	AddedPrevious30Days int            `json:"added_previous_30_days"`
	DurationSeconds     float64        `json:"duration_seconds"`
	MediaBytes          int64          `json:"media_bytes"`
	CoverBytes          int64          `json:"cover_bytes"`
	UploadTempBytes     int64          `json:"upload_temp_bytes"`
	TotalBytes          int64          `json:"total_bytes"`
	BytesLast30Days     int64          `json:"bytes_last_30_days"`
	BytesPrevious30Days int64          `json:"bytes_previous_30_days"`
	Series              []AdminDayStat `json:"series"`
}

// AdminStats aggregates audiobook counts and storage for the admin dashboard.
// The series mirrors the manager's MetricSeries window: 30 UTC days, oldest
// first, the last being now's date.
func (s *PostgresStore) AdminStats(ctx context.Context, now time.Time) (AdminStatsReport, error) {
	now = now.UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	windowStart := today.AddDate(0, 0, -(adminAudiobookSeriesDays - 1))
	last30 := now.AddDate(0, 0, -30)
	prev30 := now.AddDate(0, 0, -60)

	var stats AdminStatsReport
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
		return AdminStatsReport{}, fmt.Errorf("aggregate audiobooks: %w", err)
	}

	if err := s.pool.QueryRow(ctx, `SELECT COALESCE(SUM(received_bytes), 0) FROM audiobook_uploads`).Scan(&stats.UploadTempBytes); err != nil {
		return AdminStatsReport{}, fmt.Errorf("sum audiobook uploads: %w", err)
	}

	coverRows, err := s.pool.Query(ctx, `SELECT cover_path FROM audiobooks WHERE cover_path <> ''`)
	if err != nil {
		return AdminStatsReport{}, fmt.Errorf("list audiobook covers: %w", err)
	}
	for coverRows.Next() {
		var path string
		if err := coverRows.Scan(&path); err != nil {
			coverRows.Close()
			return AdminStatsReport{}, fmt.Errorf("scan audiobook cover: %w", err)
		}
		if info, statErr := os.Stat(path); statErr == nil && !info.IsDir() {
			stats.CoverBytes += info.Size()
		}
	}
	coverRows.Close()
	if err := coverRows.Err(); err != nil {
		return AdminStatsReport{}, fmt.Errorf("list audiobook covers: %w", err)
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
		return AdminStatsReport{}, fmt.Errorf("aggregate audiobook days: %w", err)
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
			return AdminStatsReport{}, fmt.Errorf("scan audiobook day: %w", err)
		}
		adds[date] = add
		windowCount += add.count
		windowBytes += add.bytes
	}
	dayRows.Close()
	if err := dayRows.Err(); err != nil {
		return AdminStatsReport{}, fmt.Errorf("aggregate audiobook days: %w", err)
	}

	runningCount := stats.Count - windowCount
	runningBytes := stats.MediaBytes - windowBytes
	stats.Series = make([]AdminDayStat, adminAudiobookSeriesDays)
	for index := range stats.Series {
		date := today.AddDate(0, 0, index-(adminAudiobookSeriesDays-1)).Format("2006-01-02")
		add := adds[date]
		runningCount += add.count
		runningBytes += add.bytes
		stats.Series[index] = AdminDayStat{Date: date, Count: runningCount, Bytes: runningBytes}
	}
	return stats, nil
}
