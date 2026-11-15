package audiobooks

import (
	"context"
	"fmt"
	"time"
)

const adminAudiobookDefaultSort = "created_at"

// adminAudiobookMP3Predicate matches the same rows ListStoredMP3s returns.
const adminAudiobookMP3Predicate = `(a.mime_type = 'audio/mpeg' OR lower(a.storage_path) LIKE '%.mp3')`

var adminAudiobookSortSQL = map[string]string{
	"title":      "lower(a.title)",
	"author":     "lower(a.author)",
	"format":     "format",
	"size":       "a.size_bytes",
	"duration":   "a.duration_seconds",
	"uploader":   "a.uploaded_by_user_id",
	"created_at": "a.created_at",
	"listeners":  "active_listeners",
	"hours":      "seconds_listened",
}

// adminAudiobookFilterSQL holds the WHERE fragments. conversion-failed needs
// the failed id set, bound as $1; not-converted ("awaiting conversion") is the
// MP3s minus those permanently failed, matching the dashboard count.
var adminAudiobookFilterSQL = map[string]string{
	"no-cover":          "a.cover_path = ''",
	"no-chapters":       "a.chapters = '[]'::jsonb",
	"no-series":         "a.series_id IS NULL",
	"not-converted":     "(" + adminAudiobookMP3Predicate + ") AND NOT (a.id = ANY($1))",
	"conversion-failed": "a.id = ANY($1)",
}

// AdminRow is one line of the admin audiobook library.
type AdminRow struct {
	ID              string    `json:"id"`
	Title           string    `json:"title"`
	Author          string    `json:"author"`
	Format          string    `json:"format"`
	SizeBytes       int64     `json:"size_bytes"`
	DurationSeconds float64   `json:"duration_seconds"`
	UploaderID      string    `json:"uploader_id"`
	UploaderName    string    `json:"uploader_name"`
	CreatedAt       time.Time `json:"created_at"`
	HasCover        bool      `json:"has_cover"`
	HasChapters     bool      `json:"has_chapters"`
	HasSeries       bool      `json:"has_series"`
	// ActiveListeners is the distinct users with listening in the last 30 days.
	ActiveListeners int `json:"active_listeners"`
	// SecondsListened is all-time listening across all users.
	SecondsListened float64 `json:"seconds_listened"`
}

// AdminQuery is a validated list request. Sort and Dir are keys, not
// SQL: Sort is looked up in adminAudiobookSortSQL.
type AdminQuery struct {
	Sort      string
	Dir       string
	Filter    string
	FailedIDs []string
	Limit     int
	Offset    int
	// WindowStart is the first day of the active-listener window; zero means
	// AdminWindowStart(now).
	WindowStart time.Time
}

func ValidAdminFilter(f string) bool {
	if f == "" {
		return true
	}
	_, ok := adminAudiobookFilterSQL[f]
	return ok
}

// AdminListAudiobooks returns one page of the library and the total number of
// rows matching the filter.
func (s *PostgresStore) AdminListAudiobooks(ctx context.Context, q AdminQuery) ([]AdminRow, int, error) {
	orderExpr, ok := adminAudiobookSortSQL[q.Sort]
	dir := "DESC"
	if !ok {
		orderExpr = adminAudiobookSortSQL[adminAudiobookDefaultSort]
	} else if q.Dir == "asc" {
		dir = "ASC"
	}

	var args []any
	where := ""
	if q.Filter != "" {
		frag, ok := adminAudiobookFilterSQL[q.Filter]
		if !ok {
			return nil, 0, fmt.Errorf("unknown audiobook filter %q", q.Filter)
		}
		where = "WHERE " + frag
		if q.Filter == "conversion-failed" || q.Filter == "not-converted" {
			ids := q.FailedIDs
			if ids == nil {
				ids = []string{}
			}
			args = append(args, ids)
		}
	}

	var total int
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM audiobooks a `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count admin audiobooks: %w", err)
	}

	limitArg := fmt.Sprintf("$%d", len(args)+1)
	offsetArg := fmt.Sprintf("$%d", len(args)+2)
	windowStart := q.WindowStart
	if windowStart.IsZero() {
		windowStart = AdminWindowStart(time.Now())
	}
	windowArg := fmt.Sprintf("$%d", len(args)+3)
	args = append(args, q.Limit, q.Offset, windowStart.Format("2006-01-02"))
	rows, err := s.pool.Query(ctx, `
		SELECT a.id, a.title, a.author,
			CASE WHEN `+adminAudiobookMP3Predicate+` THEN 'MP3'
				ELSE upper(COALESCE(substring(a.storage_path from '\.([^./\\]+)$'), '')) END AS format,
			a.size_bytes, a.duration_seconds, a.uploaded_by_user_id, a.created_at,
			a.cover_path <> '', a.chapters <> '[]'::jsonb, a.series_id IS NOT NULL,
			COALESCE(ls.active_listeners, 0) AS active_listeners,
			COALESCE(ls.seconds_listened, 0) AS seconds_listened
		FROM audiobooks a
		LEFT JOIN (
			SELECT audiobook_id,
				COUNT(DISTINCT user_id) FILTER (WHERE listen_date >= `+windowArg+`::date) AS active_listeners,
				SUM(seconds_listened) AS seconds_listened
			FROM audiobook_listening_stats
			GROUP BY audiobook_id
		) ls ON ls.audiobook_id = a.id
		`+where+`
		ORDER BY `+orderExpr+` `+dir+`, a.id `+dir+`
		LIMIT `+limitArg+` OFFSET `+offsetArg, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list admin audiobooks: %w", err)
	}
	defer rows.Close()
	items := []AdminRow{}
	for rows.Next() {
		var r AdminRow
		if err := rows.Scan(&r.ID, &r.Title, &r.Author, &r.Format, &r.SizeBytes, &r.DurationSeconds, &r.UploaderID, &r.CreatedAt,
			&r.HasCover, &r.HasChapters, &r.HasSeries, &r.ActiveListeners, &r.SecondsListened); err != nil {
			return nil, 0, fmt.Errorf("scan admin audiobook: %w", err)
		}
		items = append(items, r)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate admin audiobooks: %w", err)
	}
	return items, total, nil
}
