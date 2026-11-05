package client

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	adminAudiobookDefaultPageSize = 50
	adminAudiobookMaxPageSize     = 200
	adminAudiobookDefaultSort     = "created_at"
)

// adminAudiobookRow is one line of the admin audiobook library.
type adminAudiobookRow struct {
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

// adminAudiobookQuery is a validated list request. Sort and Dir are keys, not
// SQL: Sort is looked up in adminAudiobookSortSQL.
type adminAudiobookQuery struct {
	Sort      string
	Dir       string
	Filter    string
	FailedIDs []string
	Limit     int
	Offset    int
}

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
// the failed id set, bound as $1.
var adminAudiobookFilterSQL = map[string]string{
	"no-cover":          "a.cover_path = ''",
	"no-chapters":       "a.chapters = '[]'::jsonb",
	"no-series":         "a.series_id IS NULL",
	"not-converted":     adminAudiobookMP3Predicate,
	"conversion-failed": "a.id = ANY($1)",
}

func validAdminAudiobookFilter(f string) bool {
	if f == "" {
		return true
	}
	_, ok := adminAudiobookFilterSQL[f]
	return ok
}

// AdminListAudiobooks returns one page of the library and the total number of
// rows matching the filter.
func (s *postgresAudiobookStore) AdminListAudiobooks(ctx context.Context, q adminAudiobookQuery) ([]adminAudiobookRow, int, error) {
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
		if q.Filter == "conversion-failed" {
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
	args = append(args, q.Limit, q.Offset)
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
				COUNT(DISTINCT user_id) FILTER (WHERE listen_date >= (CURRENT_DATE - 30)) AS active_listeners,
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
	items := []adminAudiobookRow{}
	for rows.Next() {
		var r adminAudiobookRow
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

func (s *Server) handleAdminAudiobooks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.audiobooks == nil {
		writeJSON(w, http.StatusOK, map[string]string{"state": "unavailable"})
		return
	}
	params := r.URL.Query()
	filter := params.Get("filter")
	if !validAdminAudiobookFilter(filter) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown filter"})
		return
	}
	page, _ := strconv.Atoi(params.Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(params.Get("page_size"))
	if pageSize < 1 {
		pageSize = adminAudiobookDefaultPageSize
	}
	if pageSize > adminAudiobookMaxPageSize {
		pageSize = adminAudiobookMaxPageSize
	}
	query := adminAudiobookQuery{
		Sort:   params.Get("sort"),
		Dir:    strings.ToLower(params.Get("dir")),
		Filter: filter,
		Limit:  pageSize,
		Offset: (page - 1) * pageSize,
	}
	if filter == "conversion-failed" {
		ids, err := s.failedRemuxAudiobookIDs(r.Context())
		if err != nil {
			log.Printf("admin audiobook failed conversions failed: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "audiobook list failed"})
			return
		}
		query.FailedIDs = ids
	}
	items, total, err := s.audiobooks.AdminListAudiobooks(r.Context(), query)
	if err != nil {
		log.Printf("admin audiobook list failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "audiobook list failed"})
		return
	}
	names := s.adminUserNames(r.Context())
	for i := range items {
		name := names[items[i].UploaderID]
		if name == "" {
			name = items[i].UploaderID
		}
		items[i].UploaderName = name
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "page": page, "page_size": pageSize})
}
