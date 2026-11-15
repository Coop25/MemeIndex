package audiobooks

import (
	"context"
	"fmt"
	"time"
)

const (
	adminListeningDays = 30
	adminListeningTop  = 5
)

type AdminDaySeconds struct {
	Date    string  `json:"date"`
	Seconds float64 `json:"seconds"`
}

type AdminTopBook struct {
	ID      string  `json:"id"`
	Title   string  `json:"title"`
	Seconds float64 `json:"seconds"`
}

type AdminTopListener struct {
	UserID      string  `json:"user_id"`
	DisplayName string  `json:"display_name"`
	Seconds     float64 `json:"seconds"`
}

// AdminHealth counts audiobooks that are missing metadata or are
// waiting on / failed an MP3 to M4A conversion.
type AdminHealth struct {
	MissingCover       int `json:"missing_cover"`
	MissingChapters    int `json:"missing_chapters"`
	MissingSeries      int `json:"missing_series"`
	AwaitingConversion int `json:"awaiting_conversion"`
	FailedConversion   int `json:"failed_conversion"`
}

// AdminWindowStart is the first day (UTC midnight) of the admin 30-day
// window: today minus 29 days, so the window spans exactly 30 dates.
func AdminWindowStart(now time.Time) time.Time {
	now = now.UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	return today.AddDate(0, 0, -(adminListeningDays - 1))
}

// AdminListening aggregates listening time over the last 30 UTC days (oldest
// first, the last being the current date) plus the top books and listeners.
// Listener display names are left empty for the caller to fill.
func (s *PostgresStore) AdminListening(ctx context.Context, now time.Time) ([]AdminDaySeconds, []AdminTopBook, []AdminTopListener, error) {
	windowStart := AdminWindowStart(now)

	rows, err := s.pool.Query(ctx, `
		SELECT to_char(listen_date, 'YYYY-MM-DD'), COALESCE(SUM(seconds_listened), 0)
		FROM audiobook_listening_stats
		WHERE listen_date >= $1::date
		GROUP BY 1
	`, windowStart)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("aggregate listening days: %w", err)
	}
	perDay := map[string]float64{}
	for rows.Next() {
		var date string
		var seconds float64
		if err := rows.Scan(&date, &seconds); err != nil {
			rows.Close()
			return nil, nil, nil, fmt.Errorf("scan listening day: %w", err)
		}
		perDay[date] = seconds
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, nil, fmt.Errorf("aggregate listening days: %w", err)
	}
	daily := make([]AdminDaySeconds, adminListeningDays)
	for index := range daily {
		date := windowStart.AddDate(0, 0, index).Format("2006-01-02")
		daily[index] = AdminDaySeconds{Date: date, Seconds: perDay[date]}
	}

	bookRows, err := s.pool.Query(ctx, `
		SELECT l.audiobook_id, COALESCE(a.title, ''), SUM(l.seconds_listened) AS total
		FROM audiobook_listening_stats l
		LEFT JOIN audiobooks a ON a.id = l.audiobook_id
		WHERE l.listen_date >= $1::date
		GROUP BY l.audiobook_id, a.title
		ORDER BY total DESC, l.audiobook_id
		LIMIT $2
	`, windowStart, adminListeningTop)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("aggregate top books: %w", err)
	}
	books := []AdminTopBook{}
	for bookRows.Next() {
		var book AdminTopBook
		if err := bookRows.Scan(&book.ID, &book.Title, &book.Seconds); err != nil {
			bookRows.Close()
			return nil, nil, nil, fmt.Errorf("scan top book: %w", err)
		}
		books = append(books, book)
	}
	bookRows.Close()
	if err := bookRows.Err(); err != nil {
		return nil, nil, nil, fmt.Errorf("aggregate top books: %w", err)
	}

	listenerRows, err := s.pool.Query(ctx, `
		SELECT user_id, SUM(seconds_listened) AS total
		FROM audiobook_listening_stats
		WHERE listen_date >= $1::date
		GROUP BY user_id
		ORDER BY total DESC, user_id
		LIMIT $2
	`, windowStart, adminListeningTop)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("aggregate top listeners: %w", err)
	}
	listeners := []AdminTopListener{}
	for listenerRows.Next() {
		var listener AdminTopListener
		if err := listenerRows.Scan(&listener.UserID, &listener.Seconds); err != nil {
			listenerRows.Close()
			return nil, nil, nil, fmt.Errorf("scan top listener: %w", err)
		}
		listeners = append(listeners, listener)
	}
	listenerRows.Close()
	if err := listenerRows.Err(); err != nil {
		return nil, nil, nil, fmt.Errorf("aggregate top listeners: %w", err)
	}
	return daily, books, listeners, nil
}

// AdminHealthCounts counts audiobooks missing a cover, chapters or series, and
// the MP3s awaiting conversion. FailedConversion needs the filesystem, so it
// is filled in from Server.failedRemuxAudiobookIDs, not here.
func (s *PostgresStore) AdminHealthCounts(ctx context.Context) (AdminHealth, error) {
	var health AdminHealth
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE cover_path = ''),
			COUNT(*) FILTER (WHERE chapters = '[]'::jsonb),
			COUNT(*) FILTER (WHERE series_id IS NULL)
		FROM audiobooks
	`).Scan(&health.MissingCover, &health.MissingChapters, &health.MissingSeries)
	if err != nil {
		return AdminHealth{}, fmt.Errorf("count audiobook health: %w", err)
	}
	mp3s, err := s.ListStoredMP3s(ctx)
	if err != nil {
		return AdminHealth{}, err
	}
	health.AwaitingConversion = len(mp3s)
	return health, nil
}

// ListStoragePaths returns the stored file and cover path of every audiobook.
func (s *PostgresStore) ListStoragePaths(ctx context.Context) ([]StoragePath, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, title, storage_path, cover_path, size_bytes, uploaded_by_user_id FROM audiobooks`)
	if err != nil {
		return nil, fmt.Errorf("list audiobook storage paths: %w", err)
	}
	defer rows.Close()
	out := []StoragePath{}
	for rows.Next() {
		var p StoragePath
		if err := rows.Scan(&p.ID, &p.Title, &p.StoragePath, &p.CoverPath, &p.SizeBytes, &p.UploadedBy); err != nil {
			return nil, fmt.Errorf("scan audiobook storage path: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ListUploadTempPaths returns the temp file of every in-progress upload.
func (s *PostgresStore) ListUploadTempPaths(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT temp_path FROM audiobook_uploads`)
	if err != nil {
		return nil, fmt.Errorf("list audiobook upload temp paths: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, fmt.Errorf("scan audiobook upload temp path: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
