package client

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"memeindex/internal/manager"
)

type adminDaySeconds struct {
	Date    string  `json:"date"`
	Seconds float64 `json:"seconds"`
}

type adminTopBook struct {
	ID      string  `json:"id"`
	Title   string  `json:"title"`
	Seconds float64 `json:"seconds"`
}

type adminTopListener struct {
	UserID      string  `json:"user_id"`
	DisplayName string  `json:"display_name"`
	Seconds     float64 `json:"seconds"`
}

// adminAudiobookHealth counts audiobooks that are missing metadata or are
// waiting on / failed an MP3 to M4A conversion.
type adminAudiobookHealth struct {
	MissingCover       int `json:"missing_cover"`
	MissingChapters    int `json:"missing_chapters"`
	MissingSeries      int `json:"missing_series"`
	AwaitingConversion int `json:"awaiting_conversion"`
	FailedConversion   int `json:"failed_conversion"`
}

type adminAudiobookActivity struct {
	DailySeconds []adminDaySeconds    `json:"daily_seconds"`
	TopBooks     []adminTopBook       `json:"top_books"`
	TopListeners []adminTopListener   `json:"top_listeners"`
	Health       adminAudiobookHealth `json:"health"`
}

// adminWorkQueues are the "waiting on an admin or a worker" counts.
type adminWorkQueues struct {
	DeleteQueue     int `json:"delete_queue"`
	SuggestedTags   int `json:"suggested_tags"`
	LinkRetries     int `json:"link_retries"`
	RejectedLinks   int `json:"rejected_links"`
	ConversionQueue int `json:"conversion_queue"`
}

const (
	adminListeningDays = 30
	adminListeningTop  = 5
)

// AdminListening aggregates listening time over the last 30 UTC days (oldest
// first, the last being the current date) plus the top books and listeners.
// Listener display names are left empty for the caller to fill.
func (s *postgresAudiobookStore) AdminListening(ctx context.Context, now time.Time) ([]adminDaySeconds, []adminTopBook, []adminTopListener, error) {
	now = now.UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	windowStart := today.AddDate(0, 0, -(adminListeningDays - 1))

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
	daily := make([]adminDaySeconds, adminListeningDays)
	for index := range daily {
		date := windowStart.AddDate(0, 0, index).Format("2006-01-02")
		daily[index] = adminDaySeconds{Date: date, Seconds: perDay[date]}
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
	books := []adminTopBook{}
	for bookRows.Next() {
		var book adminTopBook
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
	listeners := []adminTopListener{}
	for listenerRows.Next() {
		var listener adminTopListener
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
func (s *postgresAudiobookStore) AdminHealthCounts(ctx context.Context) (adminAudiobookHealth, error) {
	var health adminAudiobookHealth
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE cover_path = ''),
			COUNT(*) FILTER (WHERE chapters = '[]'::jsonb),
			COUNT(*) FILTER (WHERE series_id IS NULL)
		FROM audiobooks
	`).Scan(&health.MissingCover, &health.MissingChapters, &health.MissingSeries)
	if err != nil {
		return adminAudiobookHealth{}, fmt.Errorf("count audiobook health: %w", err)
	}
	mp3s, err := s.ListStoredMP3s(ctx)
	if err != nil {
		return adminAudiobookHealth{}, err
	}
	health.AwaitingConversion = len(mp3s)
	return health, nil
}

// failedRemuxAudiobookIDs returns the IDs of stored MP3 audiobooks whose
// conversion left a failure marker next to the file.
func (s *Server) failedRemuxAudiobookIDs(ctx context.Context) ([]string, error) {
	if s.audiobooks == nil {
		return nil, nil
	}
	books, err := s.audiobooks.ListStoredMP3s(ctx)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for _, book := range books {
		if _, statErr := os.Stat(audiobookRemuxFailurePath(book.StoragePath)); statErr == nil {
			ids = append(ids, book.ID)
		}
	}
	return ids, nil
}

// audiobookActivityCache memoizes the audiobook activity aggregate for
// manager.AdminReadCacheTTL.
type audiobookActivityCache struct {
	mu       sync.Mutex
	activity adminAudiobookActivity
	cachedAt time.Time
	valid    bool
}

func cloneAdminAudiobookActivity(a adminAudiobookActivity) adminAudiobookActivity {
	a.DailySeconds = append([]adminDaySeconds(nil), a.DailySeconds...)
	a.TopBooks = append([]adminTopBook(nil), a.TopBooks...)
	a.TopListeners = append([]adminTopListener(nil), a.TopListeners...)
	return a
}

// cachedAudiobookActivity returns the listening/health aggregate (listener
// display names empty), or nil when audiobooks are not configured.
func (s *Server) cachedAudiobookActivity(ctx context.Context) (*adminAudiobookActivity, error) {
	if s.audiobooks == nil {
		return nil, nil
	}
	c := &s.audiobookActivity
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.valid && time.Since(c.cachedAt) < manager.AdminReadCacheTTL {
		out := cloneAdminAudiobookActivity(c.activity)
		return &out, nil
	}
	var activity adminAudiobookActivity
	failures := 0
	if daily, books, listeners, err := s.audiobooks.AdminListening(ctx, time.Now()); err != nil {
		failures++
		log.Printf("admin dashboard audiobook listening failed: %v", err)
		activity.DailySeconds, activity.TopBooks, activity.TopListeners = []adminDaySeconds{}, []adminTopBook{}, []adminTopListener{}
	} else {
		activity.DailySeconds, activity.TopBooks, activity.TopListeners = daily, books, listeners
	}
	if health, err := s.audiobooks.AdminHealthCounts(ctx); err != nil {
		failures++
		log.Printf("admin dashboard audiobook health failed: %v", err)
	} else {
		activity.Health = health
	}
	if failed, err := s.failedRemuxAudiobookIDs(ctx); err != nil {
		failures++
		log.Printf("admin dashboard failed conversions failed: %v", err)
	} else {
		activity.Health.FailedConversion = len(failed)
	}
	if failures == 3 {
		return nil, fmt.Errorf("all audiobook activity queries failed")
	}
	c.activity, c.cachedAt, c.valid = activity, time.Now(), true
	out := cloneAdminAudiobookActivity(activity)
	return &out, nil
}

// ListStoragePaths returns the stored file and cover path of every audiobook.
func (s *postgresAudiobookStore) ListStoragePaths(ctx context.Context) ([]audiobookStoragePath, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, title, storage_path, cover_path, size_bytes, uploaded_by_user_id FROM audiobooks`)
	if err != nil {
		return nil, fmt.Errorf("list audiobook storage paths: %w", err)
	}
	defer rows.Close()
	out := []audiobookStoragePath{}
	for rows.Next() {
		var p audiobookStoragePath
		if err := rows.Scan(&p.ID, &p.Title, &p.StoragePath, &p.CoverPath, &p.SizeBytes, &p.UploadedBy); err != nil {
			return nil, fmt.Errorf("scan audiobook storage path: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ListUploadTempPaths returns the temp file of every in-progress upload.
func (s *postgresAudiobookStore) ListUploadTempPaths(ctx context.Context) ([]string, error) {
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
