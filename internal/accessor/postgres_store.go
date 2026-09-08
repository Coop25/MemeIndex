package accessor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"

	"memeindex/internal/dbschema"
)

type PostgresStore struct {
	pool       *pgxpool.Pool
	uploadDir  string
	previewDir string
	dataDir    string
}

func NewPostgresStore(ctx context.Context, databaseURL string, dataDir string) (*PostgresStore, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, errors.New("database url is required")
	}

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	uploadDir := filepath.Join(dataDir, "uploads")
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		return nil, fmt.Errorf("create upload dir: %w", err)
	}

	pool, err := NewPool(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}

	store := &PostgresStore{
		pool:       pool,
		uploadDir:  uploadDir,
		previewDir: filepath.Join(dataDir, "thumbnails"),
		dataDir:    dataDir,
	}

	if err := store.ensureSchema(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	if err := store.importLegacyDataIfNeeded(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	if err := store.importLegacyReelSessionsIfNeeded(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	if err := store.backfillMissingContentHashes(ctx); err != nil {
		pool.Close()
		return nil, err
	}

	return store, nil
}

func (s *PostgresStore) UploadDir() string {
	return s.uploadDir
}

// Ping verifies the connection pool can reach PostgreSQL. It backs the /readyz
// endpoint so orchestration can tell a database outage apart from a healthy
// process.
func (s *PostgresStore) Ping(ctx context.Context) error {
	if s.pool == nil {
		return errors.New("postgres pool is not initialised")
	}
	return s.pool.Ping(ctx)
}

// Close releases the connection pool. It is meant to be called once during a
// graceful shutdown, after the HTTP server has stopped accepting requests.
func (s *PostgresStore) Close() {
	if s.pool != nil {
		s.pool.Close()
	}
}

func (s *PostgresStore) ThumbnailDir() string {
	return s.previewDir
}

func (s *PostgresStore) EnsurePreviewAssets() error {
	memes := s.List("", "", false, "")
	totalVideos := 0
	generated := 0
	existing := 0
	failed := 0

	for _, meme := range memes {
		if strings.HasPrefix(meme.ContentType, "video/") {
			totalVideos += 1
		}
	}

	if totalVideos == 0 {
		log.Printf("preview asset backfill: no video memes found")
		return nil
	}

	log.Printf("preview asset backfill: starting postgres video thumbnails for %d video(s)", totalVideos)

	processedVideos := 0
	for i := range memes {
		if !strings.HasPrefix(memes[i].ContentType, "video/") {
			continue
		}

		result, err := ensurePreviewAssetWithResult(s.uploadDir, s.previewDir, &memes[i])
		processedVideos += 1
		switch result {
		case previewAssetGenerated:
			generated += 1
		case previewAssetAlreadyExists:
			existing += 1
		}
		if err != nil {
			failed += 1
		}

		if processedVideos%25 == 0 || processedVideos == totalVideos {
			log.Printf(
				"preview asset backfill: processed %d/%d videos (generated=%d existing=%d failed=%d)",
				processedVideos,
				totalVideos,
				generated,
				existing,
				failed,
			)
		}

		if err != nil {
			continue
		}
	}

	log.Printf(
		"preview asset backfill: finished postgres video thumbnails (total=%d generated=%d existing=%d failed=%d)",
		totalVideos,
		generated,
		existing,
		failed,
	)
	return nil
}

func (s *PostgresStore) List(userID, query string, favoritesOnly bool, tag string) []Meme {
	ctx := context.Background()
	userID = normalizeFavoriteUserID(userID)
	query = strings.ToLower(strings.TrimSpace(query))
	tag = normalizeTag(tag)

	rows, err := s.pool.Query(ctx, `
		SELECT
			m.id,
			m.original_name,
			m.stored_name,
			m.file_path,
			m.content_type,
			m.size_bytes,
			COALESCE(array_agg(t.name ORDER BY t.name) FILTER (WHERE t.name IS NOT NULL), '{}') AS tags,
			COALESCE(m.suggested_tags, '{}') AS suggested_tags,
			COALESCE(m.auto_suggest_disabled, FALSE) AS auto_suggest_disabled,
			m.notes,
			COALESCE(m.source_url, '') AS source_url,
			EXISTS (
				SELECT 1
				FROM user_favorites uf
				WHERE uf.user_id = $1 AND uf.meme_id = m.id
			) AS favorite,
			m.created_at,
			m.updated_at
		FROM memes m
		LEFT JOIN meme_tags mt ON mt.meme_id = m.id
		LEFT JOIN tags t ON t.id = mt.tag_id
		WHERE
			COALESCE(m.hidden_from_app, FALSE) = FALSE
			AND
			(
				$2 = '' OR
				LOWER(m.original_name) LIKE '%' || $2 || '%' OR
				LOWER(m.notes) LIKE '%' || $2 || '%' OR
				LOWER(COALESCE(m.source_url, '')) LIKE '%' || $2 || '%' OR
				LOWER(m.content_type) LIKE '%' || $2 || '%' OR
				EXISTS (
					SELECT 1
					FROM meme_tags mtq
					JOIN tags tq ON tq.id = mtq.tag_id
					WHERE mtq.meme_id = m.id AND tq.name LIKE '%' || $2 || '%'
				)
			)
			AND
			(
				$3 = '' OR
				EXISTS (
					SELECT 1
					FROM meme_tags mtt
					JOIN tags tt ON tt.id = mtt.tag_id
					WHERE mtt.meme_id = m.id AND tt.name LIKE '%' || $3 || '%'
				)
			)
			AND
			(
				NOT $4 OR
				EXISTS (
					SELECT 1
					FROM user_favorites uff
					WHERE uff.user_id = $1 AND uff.meme_id = m.id
				)
			)
		GROUP BY m.id
		ORDER BY m.created_at DESC
	`, userID, query, tag, favoritesOnly)
	if err != nil {
		return nil
	}
	defer rows.Close()

	memes := make([]Meme, 0)
	for rows.Next() {
		meme, scanErr := scanMemeRow(rows)
		if scanErr != nil {
			return memes
		}
		decoratePreviewPath(&meme, s.previewDir)
		memes = append(memes, meme)
	}
	return memes
}

// memeRowSelectColumns is the projection consumed by scanMemeRow. It expects $1
// to be the favourites user id. The tag array is a correlated subquery rather
// than a join+GROUP BY so it is evaluated only for the rows actually returned.
const memeRowSelectColumns = `
	m.id,
	m.original_name,
	m.stored_name,
	m.file_path,
	m.content_type,
	m.size_bytes,
	COALESCE((
		SELECT array_agg(t.name ORDER BY t.name)
		FROM meme_tags mt
		JOIN tags t ON t.id = mt.tag_id
		WHERE mt.meme_id = m.id
	), '{}') AS tags,
	COALESCE(m.suggested_tags, '{}') AS suggested_tags,
	COALESCE(m.auto_suggest_disabled, FALSE) AS auto_suggest_disabled,
	m.notes,
	COALESCE(m.source_url, '') AS source_url,
	EXISTS (
		SELECT 1 FROM user_favorites uf
		WHERE uf.user_id = $1 AND uf.meme_id = m.id
	) AS favorite,
	m.created_at,
	m.updated_at`

// queryMemeRows resolves one page of memes: it filters, sorts, and limits the
// memes table on its own in a derived table, then builds the tag array for just
// that page. The previous form joined meme_tags/tags and GROUP BY'd before the
// LIMIT, forcing the whole filtered set through the aggregate and sort.
//
// $1 is the favourites user id; whereArgs fill $2, $3, ... in the order `where`
// references them; the LIMIT/OFFSET placeholders follow. `where` and `orderBy`
// may reference only columns of `memes m` (tag filters are EXISTS subqueries).
func (s *PostgresStore) queryMemeRows(ctx context.Context, userID, where, orderBy string, limit, offset int, whereArgs ...any) ([]Meme, error) {
	limArg := len(whereArgs) + 2
	sql := "SELECT" + memeRowSelectColumns + `
FROM (
	SELECT m.*
	FROM memes m
	WHERE ` + where + `
	ORDER BY ` + orderBy + `
	LIMIT $` + strconv.Itoa(limArg) + ` OFFSET $` + strconv.Itoa(limArg+1) + `
) m
ORDER BY ` + orderBy

	args := make([]any, 0, len(whereArgs)+3)
	args = append(args, normalizeFavoriteUserID(userID))
	args = append(args, whereArgs...)
	args = append(args, limit, offset)

	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	memes := make([]Meme, 0)
	for rows.Next() {
		meme, scanErr := scanMemeRow(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		decoratePreviewPath(&meme, s.previewDir)
		memes = append(memes, meme)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return memes, nil
}

// memeCounts resolves the facet counts over the memes matched by where (which
// references m and expects $1 = favourites user id, plus any positional args in
// args[1:]). The four media buckets are mutually exclusive and sum to Total,
// matching the in-memory buildMemeCounts classification exactly.
func (s *PostgresStore) memeCounts(ctx context.Context, where string, args []any) (MemeCategoryCounts, error) {
	sql := `
SELECT
	COUNT(*),
	COUNT(*) FILTER (WHERE EXISTS (SELECT 1 FROM user_favorites uf WHERE uf.user_id = $1 AND uf.meme_id = m.id)),
	COUNT(*) FILTER (WHERE m.content_type LIKE 'video/%'),
	COUNT(*) FILTER (WHERE m.content_type NOT LIKE 'video/%' AND m.content_type LIKE 'image/%'),
	COUNT(*) FILTER (WHERE m.content_type NOT LIKE 'video/%' AND m.content_type NOT LIKE 'image/%' AND (m.content_type = 'audio/mpeg' OR LOWER(m.original_name) LIKE '%.mp3')),
	COUNT(*) FILTER (WHERE m.content_type NOT LIKE 'video/%' AND m.content_type NOT LIKE 'image/%' AND NOT (m.content_type = 'audio/mpeg' OR LOWER(m.original_name) LIKE '%.mp3')),
	COUNT(*) FILTER (WHERE NOT EXISTS (SELECT 1 FROM meme_tags mtc WHERE mtc.meme_id = m.id))
FROM memes m
WHERE ` + where

	var c MemeCategoryCounts
	if err := s.pool.QueryRow(ctx, sql, args...).Scan(
		&c.Total, &c.Favorites, &c.Videos, &c.Images, &c.MP3s, &c.Files, &c.Untagged,
	); err != nil {
		return MemeCategoryCounts{}, err
	}
	return c, nil
}

// QueryMemes resolves one browse/search page entirely in Postgres: filter, facet
// counts, media-class view, favourites, sort, and limit/offset. It replaces the
// previous path of loading every row and paging in the application.
func (s *PostgresStore) QueryMemes(q MemeQuery) (MemeQueryPage, error) {
	ctx := context.Background()
	uid := normalizeFavoriteUserID(q.UserID)
	search := ParseMemeSearch(q.Search)

	offset := q.Offset
	if offset < 0 {
		offset = 0
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 72
	}

	where, filterArgs, _ := buildMemeWhere(search, q.Tag, 2)

	counts, err := s.memeCounts(ctx, where, append([]any{uid}, filterArgs...))
	if err != nil {
		return MemeQueryPage{}, err
	}

	pageWhere := where
	if vc := memeViewCondition(q.View, 1); vc != "" {
		pageWhere += "\n  AND " + vc
	}
	if q.FavoritesOnly {
		pageWhere += "\n  AND EXISTS (SELECT 1 FROM user_favorites uf WHERE uf.user_id = $1 AND uf.meme_id = m.id)"
	}

	// Fetch one extra row to decide HasMore without a second query.
	memes, err := s.queryMemeRows(ctx, uid, pageWhere, memeSortOrder(q.Sort), limit+1, offset, filterArgs...)
	if err != nil {
		return MemeQueryPage{}, err
	}

	page := MemeQueryPage{Counts: counts, Memes: memes}
	if len(page.Memes) > limit {
		page.Memes = page.Memes[:limit]
		page.HasMore = true
	}
	page.NextOffset = offset + len(page.Memes)
	return page, nil
}

// TagCounts returns the most-used tags across visible memes, most frequent
// first, ties broken by name.
func (s *PostgresStore) TagCounts(limit int) ([]TagCount, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.pool.Query(context.Background(), `
		SELECT t.name, COUNT(*) AS c
		FROM meme_tags mt
		JOIN tags t ON t.id = mt.tag_id
		JOIN memes m ON m.id = mt.meme_id
		WHERE COALESCE(m.hidden_from_app, FALSE) = FALSE
		GROUP BY t.id, t.name
		ORDER BY c DESC, t.name ASC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]TagCount, 0, limit)
	for rows.Next() {
		var tc TagCount
		if err := rows.Scan(&tc.Name, &tc.Count); err != nil {
			return nil, err
		}
		out = append(out, tc)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// TagUsageCounts returns, for every tag on a visible meme, how many visible
// memes carry it. It backs the admin tag-hygiene report.
func (s *PostgresStore) TagUsageCounts() (map[string]int, error) {
	rows, err := s.pool.Query(context.Background(), `
		SELECT t.name, COUNT(*) AS c
		FROM meme_tags mt
		JOIN tags t ON t.id = mt.tag_id
		JOIN memes m ON m.id = mt.meme_id
		WHERE COALESCE(m.hidden_from_app, FALSE) = FALSE
		GROUP BY t.name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]int{}
	for rows.Next() {
		var name string
		var count int
		if err := rows.Scan(&name, &count); err != nil {
			return nil, err
		}
		out[name] = count
	}
	return out, rows.Err()
}

const visibleMemePredicate = "COALESCE(m.hidden_from_app, FALSE) = FALSE"

// UntaggedWithoutSuggestionsCount counts visible memes with no tags, no stored
// suggestions, and auto-suggest still enabled - the memes the suggestion worker
// would pick up.
func (s *PostgresStore) UntaggedWithoutSuggestionsCount() (int, error) {
	var n int
	err := s.pool.QueryRow(context.Background(), `
		SELECT COUNT(*)
		FROM memes m
		WHERE `+visibleMemePredicate+`
		  AND COALESCE(m.auto_suggest_disabled, FALSE) = FALSE
		  AND COALESCE(cardinality(m.suggested_tags), 0) = 0
		  AND NOT EXISTS (SELECT 1 FROM meme_tags mt WHERE mt.meme_id = m.id)
	`).Scan(&n)
	return n, err
}

// PendingSuggestionMemes returns the total count of visible memes that carry
// stored suggestions and one newest-first page of them (id, name, suggestions).
func (s *PostgresStore) PendingSuggestionMemes(offset, limit int) (int, []Meme, error) {
	ctx := context.Background()
	if offset < 0 {
		offset = 0
	}
	if limit < 0 {
		limit = 0
	}

	const pending = visibleMemePredicate + " AND COALESCE(cardinality(m.suggested_tags), 0) > 0"

	var total int
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM memes m WHERE `+pending).Scan(&total); err != nil {
		return 0, nil, err
	}
	if limit == 0 || offset >= total {
		return total, nil, nil
	}

	rows, err := s.pool.Query(ctx, `
		SELECT m.id, m.original_name, COALESCE(m.suggested_tags, '{}')
		FROM memes m
		WHERE `+pending+`
		ORDER BY m.created_at DESC, m.id ASC
		LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()

	out := make([]Meme, 0, limit)
	for rows.Next() {
		var meme Meme
		var suggested []string
		if err := rows.Scan(&meme.ID, &meme.OriginalName, &suggested); err != nil {
			return 0, nil, err
		}
		meme.SuggestedTags = normalizeTags(suggested)
		out = append(out, meme)
	}
	return total, out, rows.Err()
}

// MergeTag repoints every meme_tags link from sourceTag onto targetTag in a
// single transaction, creating targetTag if needed and deleting sourceTag. It
// returns the number of memes that carried sourceTag and writes the same
// tag_removed / tag_added audit trail the per-meme path produced.
func (s *PostgresStore) MergeTag(sourceTag, targetTag string, actor AuditActor) (int, error) {
	ctx := context.Background()
	sourceTag = normalizeTag(sourceTag)
	targetTag = normalizeTag(targetTag)
	if sourceTag == "" || targetTag == "" {
		return 0, errors.New("source and target tags are required")
	}
	if sourceTag == targetTag {
		return 0, nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var sourceID int64
	if err := tx.QueryRow(ctx, `SELECT id FROM tags WHERE name = $1`, sourceTag).Scan(&sourceID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil
		}
		return 0, err
	}

	affected, err := scanIDList(ctx, tx, `SELECT meme_id FROM meme_tags WHERE tag_id = $1 ORDER BY meme_id`, sourceID)
	if err != nil {
		return 0, err
	}
	if len(affected) == 0 {
		if _, err := tx.Exec(ctx, `DELETE FROM tags WHERE id = $1`, sourceID); err != nil {
			return 0, err
		}
		return 0, tx.Commit(ctx)
	}

	var targetID int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO tags (name) VALUES ($1)
		ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
		RETURNING id
	`, targetTag).Scan(&targetID); err != nil {
		return 0, err
	}

	alreadyTagged, err := scanIDList(ctx, tx, `SELECT meme_id FROM meme_tags WHERE tag_id = $1`, targetID)
	if err != nil {
		return 0, err
	}
	hasTarget := make(map[string]struct{}, len(alreadyTagged))
	for _, id := range alreadyTagged {
		hasTarget[id] = struct{}{}
	}

	if _, err := tx.Exec(ctx, `
		UPDATE meme_tags
		SET tag_id = $2
		WHERE tag_id = $1
		  AND NOT EXISTS (
			SELECT 1 FROM meme_tags existing
			WHERE existing.meme_id = meme_tags.meme_id AND existing.tag_id = $2
		  )
	`, sourceID, targetID); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM meme_tags WHERE tag_id = $1`, sourceID); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM tags WHERE id = $1`, sourceID); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `UPDATE memes SET updated_at = NOW() WHERE id = ANY($1::text[])`, affected); err != nil {
		return 0, err
	}

	for _, id := range affected {
		if err := s.insertAuditLog(ctx, tx, id, "tag_removed", actor, fmt.Sprintf("Removed tag %q", sourceTag)); err != nil {
			return 0, err
		}
		if _, ok := hasTarget[id]; !ok {
			if err := s.insertAuditLog(ctx, tx, id, "tag_added", actor, fmt.Sprintf("Added tag %q", targetTag)); err != nil {
				return 0, err
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return len(affected), nil
}

func scanIDList(ctx context.Context, q queryable, sql string, args ...any) ([]string, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// MemeDashboard resolves the user home screen summary in Postgres instead of
// scanning the whole archive into memory. The seven independent reads are run
// concurrently on separate pooled connections; the first error cancels the rest.
func (s *PostgresStore) MemeDashboard(userID string) (MemeDashboardData, error) {
	uid := normalizeFavoriteUserID(userID)
	const visible = "COALESCE(m.hidden_from_app, FALSE) = FALSE"

	var data MemeDashboardData
	g, ctx := errgroup.WithContext(context.Background())

	g.Go(func() error {
		counts, err := s.memeCounts(ctx, visible, []any{uid})
		if err != nil {
			return err
		}
		data.Counts = counts
		data.TotalItems = counts.Total
		data.Favorites = counts.Favorites
		return nil
	})
	g.Go(func() error {
		return s.pool.QueryRow(ctx,
			`SELECT COALESCE(SUM(size_bytes), 0)::bigint FROM memes m WHERE `+visible,
		).Scan(&data.StorageBytes)
	})
	g.Go(func() error {
		return s.pool.QueryRow(ctx, `
			SELECT COUNT(DISTINCT t.id)
			FROM meme_tags mt
			JOIN tags t ON t.id = mt.tag_id
			JOIN memes m ON m.id = mt.meme_id
			WHERE `+visible,
		).Scan(&data.TagCount)
	})
	g.Go(func() error {
		rows, err := s.queryMemeRows(ctx, uid, visible, "m.created_at DESC, m.id ASC", 6, 0)
		data.RecentItems = rows
		return err
	})
	g.Go(func() error {
		rows, err := s.queryMemeRows(ctx, uid,
			visible+" AND EXISTS (SELECT 1 FROM user_favorites uf WHERE uf.user_id = $1 AND uf.meme_id = m.id)",
			"m.created_at DESC, m.id ASC", 6, 0,
		)
		data.FavoriteItems = rows
		return err
	})
	g.Go(func() error {
		rows, err := s.queryMemeRows(ctx, uid, visible, "random()", 6, 0)
		data.RandomItems = rows
		return err
	})
	g.Go(func() error {
		tags, err := s.TagCounts(5)
		data.TopTags = tags
		return err
	})

	if err := g.Wait(); err != nil {
		return MemeDashboardData{}, err
	}
	return data, nil
}

func (s *PostgresStore) SuggestTags(prefix string, limit int) []string {
	ctx := context.Background()
	prefix = normalizeTag(prefix)
	if limit <= 0 {
		limit = 8
	}

	rows, err := s.pool.Query(ctx, `
		SELECT t.name
		FROM tags t
		LEFT JOIN meme_tags mt ON mt.tag_id = t.id
		WHERE $1 = '' OR t.name LIKE '%' || $1 || '%'
		GROUP BY t.id, t.name
		ORDER BY COUNT(mt.meme_id) DESC, t.name
		LIMIT $2
	`, prefix, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()

	tags := make([]string, 0, limit)
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return tags
		}
		tags = append(tags, tag)
	}
	return tags
}

func (s *PostgresStore) GetByID(userID, id string) (Meme, error) {
	ctx := context.Background()
	return s.getByID(ctx, s.pool, normalizeFavoriteUserID(userID), strings.TrimSpace(id))
}

func (s *PostgresStore) GetAnyByID(id string) (Meme, error) {
	ctx := context.Background()
	row := s.pool.QueryRow(ctx, `
		SELECT
			m.id,
			m.original_name,
			m.stored_name,
			m.file_path,
			m.content_type,
			m.size_bytes,
			COALESCE(array_agg(t.name ORDER BY t.name) FILTER (WHERE t.name IS NOT NULL), '{}') AS tags,
			COALESCE(m.suggested_tags, '{}') AS suggested_tags,
			COALESCE(m.auto_suggest_disabled, FALSE) AS auto_suggest_disabled,
			m.notes,
			COALESCE(m.source_url, '') AS source_url,
			FALSE AS favorite,
			m.created_at,
			m.updated_at
		FROM memes m
		LEFT JOIN meme_tags mt ON mt.meme_id = m.id
		LEFT JOIN tags t ON t.id = mt.tag_id
		WHERE m.id = $1
		GROUP BY m.id
	`, strings.TrimSpace(id))

	meme, err := scanMemeRow(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Meme{}, os.ErrNotExist
		}
		return Meme{}, err
	}
	decoratePreviewPath(&meme, s.previewDir)
	return meme, nil
}

func (s *PostgresStore) ListSuggestedTags(id string) ([]string, error) {
	var tags []string
	err := s.pool.QueryRow(context.Background(), `
		SELECT COALESCE(suggested_tags, '{}')
		FROM memes
		WHERE id = $1
	`, strings.TrimSpace(id)).Scan(&tags)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, os.ErrNotExist
		}
		return nil, err
	}
	return normalizeTags(tags), nil
}

func (s *PostgresStore) ReplaceSuggestedTags(id string, tags []string) error {
	commandTag, err := s.pool.Exec(context.Background(), `
		UPDATE memes
		SET suggested_tags = $2,
			auto_suggest_disabled = CASE WHEN cardinality($2::text[]) > 0 THEN FALSE ELSE auto_suggest_disabled END,
			updated_at = NOW()
		WHERE id = $1
	`, strings.TrimSpace(id), normalizeTags(tags))
	if err != nil {
		return err
	}
	if commandTag.RowsAffected() == 0 {
		return os.ErrNotExist
	}
	return nil
}

// SetSearchText stores the derived search blob for a meme. It deliberately does
// not touch updated_at: this is background-derived data, and the same
// tag-suggestion pass already bumped the row via ReplaceSuggestedTags.
func (s *PostgresStore) SetSearchText(id, text string) error {
	commandTag, err := s.pool.Exec(context.Background(), `
		UPDATE memes
		SET search_text = $2
		WHERE id = $1
	`, strings.TrimSpace(id), strings.TrimSpace(text))
	if err != nil {
		return err
	}
	if commandTag.RowsAffected() == 0 {
		return os.ErrNotExist
	}
	return nil
}

func (s *PostgresStore) SetAutoSuggestDisabled(id string, disabled bool) error {
	commandTag, err := s.pool.Exec(context.Background(), `
		UPDATE memes
		SET auto_suggest_disabled = $2,
			updated_at = NOW()
		WHERE id = $1
	`, strings.TrimSpace(id), disabled)
	if err != nil {
		return err
	}
	if commandTag.RowsAffected() == 0 {
		return os.ErrNotExist
	}
	return nil
}

func (s *PostgresStore) Random(excludedIDs []string) (Meme, error) {
	ctx := context.Background()

	normalizedExcluded := make([]string, 0, len(excludedIDs))
	for _, id := range excludedIDs {
		trimmed := strings.TrimSpace(id)
		if trimmed == "" {
			continue
		}
		normalizedExcluded = append(normalizedExcluded, trimmed)
	}

	var id string
	err := s.pool.QueryRow(ctx, `
		SELECT m.id
		FROM memes m
		WHERE COALESCE(m.hidden_from_app, FALSE) = FALSE
			AND NOT (m.id = ANY($1::text[]))
		ORDER BY random()
		LIMIT 1
	`, normalizedExcluded).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		err = s.pool.QueryRow(ctx, `
			SELECT m.id
			FROM memes m
			WHERE COALESCE(m.hidden_from_app, FALSE) = FALSE
			ORDER BY random()
			LIMIT 1
		`).Scan(&id)
	}
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Meme{}, os.ErrNotExist
		}
		return Meme{}, err
	}

	return s.getByID(ctx, s.pool, normalizeFavoriteUserID(""), id)
}

func (s *PostgresStore) Create(input CreateInput) (Meme, error) {
	id := uuid.Must(uuid.NewV6()).String()
	ext := filepath.Ext(input.Filename)
	storedName := id + ext
	targetPath := filepath.Join(s.uploadDir, storedName)

	dst, err := os.Create(targetPath)
	if err != nil {
		return Meme{}, fmt.Errorf("create upload file: %w", err)
	}

	hasher := newContentHashWriter()
	size, err := io.Copy(io.MultiWriter(dst, hasher), input.File)
	closeErr := dst.Close()
	if err != nil {
		_ = os.Remove(targetPath)
		return Meme{}, fmt.Errorf("write upload file: %w", err)
	}
	if closeErr != nil {
		_ = os.Remove(targetPath)
		return Meme{}, fmt.Errorf("close upload file: %w", closeErr)
	}

	now := time.Now().UTC()
	meme := Meme{
		ID:                  id,
		OriginalName:        input.Filename,
		StoredName:          storedName,
		FilePath:            "/uploads/" + storedName,
		ContentType:         detectContentType(input.Header, input.ContentType, input.Filename),
		ContentHash:         contentHashString(hasher),
		SizeBytes:           size,
		Tags:                normalizeTags(input.Tags),
		SuggestedTags:       []string{},
		AutoSuggestDisabled: false,
		Notes:               strings.TrimSpace(input.Notes),
		SourceURL:           strings.TrimSpace(input.SourceURL),
		CreatedAt:           now,
		UpdatedAt:           now,
	}

	if err := ensurePreviewAsset(s.uploadDir, s.previewDir, &meme); err != nil {
		// Thumbnail generation is best-effort.
	}

	ctx := context.Background()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		_ = os.Remove(targetPath)
		return Meme{}, err
	}
	defer tx.Rollback(ctx)

	if existing, err := s.getByHash(ctx, meme.ContentHash); err == nil {
		_ = os.Remove(targetPath)
		return Meme{}, &DuplicateMemeError{Existing: existing}
	} else if !errors.Is(err, os.ErrNotExist) {
		_ = os.Remove(targetPath)
		return Meme{}, err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO memes (
			id, original_name, stored_name, file_path, content_type, content_hash, size_bytes, notes, source_url, suggested_tags, auto_suggest_disabled, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`, meme.ID, meme.OriginalName, meme.StoredName, meme.FilePath, meme.ContentType, meme.ContentHash, meme.SizeBytes, meme.Notes, meme.SourceURL, meme.SuggestedTags, meme.AutoSuggestDisabled, meme.CreatedAt, meme.UpdatedAt); err != nil {
		_ = os.Remove(targetPath)
		if pgErr, ok := err.(*pgconn.PgError); ok && pgErr.Code == "23505" {
			if existing, lookupErr := s.getByHash(ctx, meme.ContentHash); lookupErr == nil {
				return Meme{}, &DuplicateMemeError{Existing: existing}
			}
		}
		return Meme{}, err
	}

	if err := s.replaceTags(ctx, tx, meme.ID, meme.Tags); err != nil {
		_ = os.Remove(targetPath)
		return Meme{}, err
	}

	if err := s.insertAuditLog(ctx, tx, meme.ID, "uploaded", input.Actor, fmt.Sprintf("Uploaded %s", meme.OriginalName)); err != nil {
		_ = os.Remove(targetPath)
		return Meme{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		_ = os.Remove(targetPath)
		return Meme{}, err
	}

	return meme, nil
}

func (s *PostgresStore) Update(userID, id string, update MemeUpdate) (Meme, error) {
	ctx := context.Background()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Meme{}, err
	}
	defer tx.Rollback(ctx)

	id = strings.TrimSpace(id)
	update.Tags = normalizeTags(update.Tags)
	update.Notes = strings.TrimSpace(update.Notes)

	existing, err := s.getByID(ctx, tx, userID, id)
	if err != nil {
		return Meme{}, err
	}

	commandTag, err := tx.Exec(ctx, `
		UPDATE memes
		SET notes = $2, updated_at = $3
		WHERE id = $1
	`, id, update.Notes, time.Now().UTC())
	if err != nil {
		return Meme{}, err
	}
	if commandTag.RowsAffected() == 0 {
		return Meme{}, os.ErrNotExist
	}

	if err := s.replaceTags(ctx, tx, id, update.Tags); err != nil {
		return Meme{}, err
	}
	if err := s.setFavoriteInExecutor(ctx, tx, userID, id, update.Favorite); err != nil {
		return Meme{}, err
	}

	addedTags, removedTags := diffAuditTags(existing.Tags, update.Tags)
	for _, tag := range addedTags {
		if err := s.insertAuditLog(ctx, tx, id, "tag_added", update.Actor, fmt.Sprintf("Added tag %q", tag)); err != nil {
			return Meme{}, err
		}
	}
	for _, tag := range removedTags {
		if err := s.insertAuditLog(ctx, tx, id, "tag_removed", update.Actor, fmt.Sprintf("Removed tag %q", tag)); err != nil {
			return Meme{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return Meme{}, err
	}

	return s.GetByID(userID, id)
}

func (s *PostgresStore) SetFavorite(userID, id string, favorite bool) (Meme, error) {
	ctx := context.Background()
	if err := s.setFavoriteInExecutor(ctx, s.pool, userID, strings.TrimSpace(id), favorite); err != nil {
		return Meme{}, err
	}
	return s.GetByID(userID, id)
}

func (s *PostgresStore) SetFavoriteWithActor(userID, id string, favorite bool, actor AuditActor) (Meme, error) {
	ctx := context.Background()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Meme{}, err
	}
	defer tx.Rollback(ctx)

	if err := s.setFavoriteInExecutor(ctx, tx, userID, strings.TrimSpace(id), favorite); err != nil {
		return Meme{}, err
	}
	action := "favorited"
	description := "Added meme to favorites"
	if !favorite {
		action = "unfavorited"
		description = "Removed meme from favorites"
	}
	if err := s.insertAuditLog(ctx, tx, strings.TrimSpace(id), action, actor, description); err != nil {
		return Meme{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Meme{}, err
	}
	return s.GetByID(userID, id)
}

func (s *PostgresStore) TotalFavoriteAssignments() (int, error) {
	var total int
	if err := s.pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM user_favorites`).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func (s *PostgresStore) FavoriteActivitySince(since time.Time) ([]AdminFavoriteActivity, error) {
	rows, err := s.pool.Query(context.Background(), `
		SELECT
			date_trunc('day', created_at) AS activity_day,
			COUNT(*) FILTER (WHERE action = 'favorited') AS added,
			COUNT(*) FILTER (WHERE action = 'unfavorited') AS removed
		FROM meme_audit_logs
		WHERE created_at >= $1
			AND action IN ('favorited', 'unfavorited')
		GROUP BY activity_day
		ORDER BY activity_day
	`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	activity := []AdminFavoriteActivity{}
	for rows.Next() {
		var day AdminFavoriteActivity
		if err := rows.Scan(&day.Date, &day.Added, &day.Removed); err != nil {
			return nil, err
		}
		activity = append(activity, day)
	}
	return activity, rows.Err()
}

func (s *PostgresStore) Delete(input DeleteInput) (DeleteResult, error) {
	ctx := context.Background()
	id := strings.TrimSpace(input.ID)
	var storedName string
	err := s.pool.QueryRow(ctx, `SELECT stored_name FROM memes WHERE id = $1`, id).Scan(&storedName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return DeleteResult{}, os.ErrNotExist
		}
		return DeleteResult{}, err
	}

	if !input.Actor.IsSuperAdmin {
		_, err := s.pool.Exec(ctx, `
			UPDATE memes
			SET hidden_from_app = TRUE,
				pending_delete = TRUE,
				delete_requested_by_user_id = $2,
				delete_requested_at = NOW(),
				updated_at = NOW()
			WHERE id = $1
		`, id, strings.TrimSpace(input.Actor.UserID))
		if err != nil {
			return DeleteResult{}, err
		}
		if err := s.insertAuditLog(ctx, s.pool, id, "delete_requested", input.Actor, "Requested delete approval"); err != nil {
			return DeleteResult{}, err
		}
		return DeleteResult{PendingApproval: true}, nil
	}

	commandTag, err := s.pool.Exec(ctx, `DELETE FROM memes WHERE id = $1`, id)
	if err != nil {
		return DeleteResult{}, err
	}
	if commandTag.RowsAffected() == 0 {
		return DeleteResult{}, os.ErrNotExist
	}

	if err := s.insertAuditLog(ctx, s.pool, id, "deleted", input.Actor, "Deleted meme"); err != nil {
		return DeleteResult{}, err
	}

	targetPath := filepath.Join(s.uploadDir, storedName)
	if err := os.Remove(targetPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return DeleteResult{}, fmt.Errorf("remove upload file: %w", err)
	}
	thumbnailPath := filepath.Join(s.previewDir, thumbnailFileName(storedName))
	if err := os.Remove(thumbnailPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return DeleteResult{}, fmt.Errorf("remove thumbnail file: %w", err)
	}
	return DeleteResult{Deleted: true}, nil
}

func (s *PostgresStore) LoadReelSessions() (map[string]ReelSessionRecord, error) {
	rows, err := s.pool.Query(context.Background(), `
		SELECT id, history, position, last_activity
		FROM reel_sessions
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sessions := map[string]ReelSessionRecord{}
	for rows.Next() {
		var (
			id           string
			historyJSON  []byte
			position     int
			lastActivity time.Time
			history      []string
		)
		if err := rows.Scan(&id, &historyJSON, &position, &lastActivity); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(historyJSON, &history); err != nil {
			return nil, err
		}
		sessions[id] = ReelSessionRecord{
			History:      history,
			Position:     position,
			LastActivity: lastActivity,
		}
	}
	return sessions, rows.Err()
}

func (s *PostgresStore) SaveReelSession(sessionID string, session ReelSessionRecord) error {
	historyJSON, err := json.Marshal(session.History)
	if err != nil {
		return err
	}

	_, err = s.pool.Exec(context.Background(), `
		INSERT INTO reel_sessions (id, history, position, last_activity)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (id) DO UPDATE
		SET history = EXCLUDED.history,
			position = EXCLUDED.position,
			last_activity = EXCLUDED.last_activity
	`, strings.TrimSpace(sessionID), historyJSON, session.Position, session.LastActivity.UTC())
	return err
}

func (s *PostgresStore) DeleteReelSession(sessionID string) error {
	_, err := s.pool.Exec(context.Background(), `DELETE FROM reel_sessions WHERE id = $1`, strings.TrimSpace(sessionID))
	return err
}

func (s *PostgresStore) CleanupStaleReelSessions(before time.Time) error {
	_, err := s.pool.Exec(context.Background(), `DELETE FROM reel_sessions WHERE last_activity < $1`, before.UTC())
	return err
}

func (s *PostgresStore) GetOrCreateMemeShare(memeID, userID string, now, expiresAt time.Time) (MemeShareState, error) {
	var share MemeShareState
	err := s.pool.QueryRow(context.Background(), `
		UPDATE memes
		SET share_generation = CASE WHEN share_expires_at > $2 THEN share_generation ELSE share_generation + 1 END,
			share_expires_at = CASE WHEN share_expires_at > $2 THEN share_expires_at ELSE $3 END,
			shared_at = CASE WHEN share_expires_at > $2 THEN shared_at ELSE $2 END,
			shared_by_user_id = CASE WHEN share_expires_at > $2 THEN shared_by_user_id ELSE $4 END
		WHERE id = $1 AND COALESCE(hidden_from_app, FALSE) = FALSE
		RETURNING id, share_generation, shared_by_user_id, shared_at, share_expires_at
	`, strings.TrimSpace(memeID), now.UTC(), expiresAt.UTC(), strings.TrimSpace(userID)).Scan(&share.MemeID, &share.Generation, &share.SharedByUserID, &share.SharedAt, &share.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return MemeShareState{}, os.ErrNotExist
	}
	return share, err
}

func (s *PostgresStore) GetMemeShareState(memeID string) (MemeShareState, error) {
	var share MemeShareState
	err := s.pool.QueryRow(context.Background(), `
		SELECT id, share_generation, shared_by_user_id, COALESCE(shared_at, 'epoch'::timestamptz), COALESCE(share_expires_at, 'epoch'::timestamptz)
		FROM memes WHERE id = $1 AND COALESCE(hidden_from_app, FALSE) = FALSE
	`, strings.TrimSpace(memeID)).Scan(&share.MemeID, &share.Generation, &share.SharedByUserID, &share.SharedAt, &share.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return MemeShareState{}, os.ErrNotExist
	}
	return share, err
}

func (s *PostgresStore) ListActiveMemeShares(now time.Time) ([]MemeShareState, error) {
	rows, err := s.pool.Query(context.Background(), `
		SELECT id, share_generation, shared_by_user_id, shared_at, share_expires_at
		FROM memes
		WHERE share_expires_at > $1 AND COALESCE(hidden_from_app, FALSE) = FALSE
		ORDER BY shared_at DESC
	`, now.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	shares := []MemeShareState{}
	for rows.Next() {
		var share MemeShareState
		if err := rows.Scan(&share.MemeID, &share.Generation, &share.SharedByUserID, &share.SharedAt, &share.ExpiresAt); err != nil {
			return nil, err
		}
		shares = append(shares, share)
	}
	return shares, rows.Err()
}

func (s *PostgresStore) RevokeMemeShare(memeID string) error {
	tag, err := s.pool.Exec(context.Background(), `
		UPDATE memes
		SET share_generation = share_generation + 1, share_expires_at = NULL
		WHERE id = $1
	`, strings.TrimSpace(memeID))
	if err == nil && tag.RowsAffected() == 0 {
		return os.ErrNotExist
	}
	return err
}

func (s *PostgresStore) RevokeAllMemeShares(now time.Time) (int, error) {
	tag, err := s.pool.Exec(context.Background(), `
		UPDATE memes
		SET share_generation = share_generation + 1, share_expires_at = NULL
		WHERE share_expires_at > $1 AND COALESCE(hidden_from_app, FALSE) = FALSE
	`, now.UTC())
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

func (s *PostgresStore) ensureSchema(ctx context.Context) error {
	if err := dbschema.Apply(ctx, s.pool,
		"001_memes_core.sql",
		"002_memes_compat.sql",
		"006_memes_source_url.sql",
		"007_memes_suggested_tags.sql",
		"008_memes_auto_suggest_disabled.sql",
		"009_meme_shares.sql",
		"010_meme_query_indexes.sql",
		"012_meme_search_text.sql",
	); err != nil {
		return fmt.Errorf("ensure schema: %w", err)
	}
	// Trigram search indexes are a speed-up, not a correctness requirement, and
	// need the pg_trgm extension a restricted role may not be able to create.
	dbschema.ApplyOptional(ctx, s.pool, "011_meme_search_trgm.sql")
	return nil
}

func (s *PostgresStore) importLegacyDataIfNeeded(ctx context.Context) error {
	var count int
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM memes`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	memes, err := s.readLegacyMemes()
	if err != nil {
		return err
	}
	favoritesByUser, err := s.readLegacyFavorites()
	if err != nil {
		return err
	}
	if len(memes) == 0 && len(favoritesByUser) == 0 {
		return nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	for _, meme := range memes {
		if _, err := tx.Exec(ctx, `
			INSERT INTO memes (
				id, original_name, stored_name, file_path, content_type, content_hash, size_bytes, notes, source_url, suggested_tags, auto_suggest_disabled, created_at, updated_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
			ON CONFLICT (id) DO NOTHING
		`, meme.ID, meme.OriginalName, meme.StoredName, meme.FilePath, meme.ContentType, meme.ContentHash, meme.SizeBytes, strings.TrimSpace(meme.Notes), strings.TrimSpace(meme.SourceURL), normalizeTags(meme.SuggestedTags), meme.AutoSuggestDisabled, meme.CreatedAt, meme.UpdatedAt); err != nil {
			return err
		}
		if err := s.replaceTags(ctx, tx, meme.ID, normalizeTags(meme.Tags)); err != nil {
			return err
		}
	}

	for userID, memeIDs := range favoritesByUser {
		normalizedUserID := normalizeFavoriteUserID(userID)
		for _, memeID := range memeIDs {
			if _, err := tx.Exec(ctx, `
				INSERT INTO user_favorites (user_id, meme_id)
				VALUES ($1, $2)
				ON CONFLICT DO NOTHING
			`, normalizedUserID, strings.TrimSpace(memeID)); err != nil {
				return err
			}
		}
	}

	return tx.Commit(ctx)
}

func (s *PostgresStore) backfillMissingContentHashes(ctx context.Context) error {
	rows, err := s.pool.Query(ctx, `
		SELECT id, stored_name
		FROM memes
		WHERE content_hash IS NULL OR content_hash = ''
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type missingHash struct {
		id         string
		storedName string
	}
	var missing []missingHash
	for rows.Next() {
		var item missingHash
		if err := rows.Scan(&item.id, &item.storedName); err != nil {
			return err
		}
		missing = append(missing, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, item := range missing {
		hash, err := computeFileHash(filepath.Join(s.uploadDir, item.storedName))
		if err != nil {
			continue
		}
		if _, err := s.pool.Exec(ctx, `
			UPDATE memes
			SET content_hash = $2
			WHERE id = $1 AND (content_hash IS NULL OR content_hash = '')
		`, item.id, hash); err != nil {
			return err
		}
	}
	return nil
}

func (s *PostgresStore) importLegacyReelSessionsIfNeeded(ctx context.Context) error {
	var count int
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM reel_sessions`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	payload, err := os.ReadFile(filepath.Join(s.dataDir, "reel_sessions.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read legacy reel sessions: %w", err)
	}
	if len(payload) == 0 {
		return nil
	}

	var raw map[string]ReelSessionRecord
	if err := json.Unmarshal(payload, &raw); err != nil {
		return fmt.Errorf("decode legacy reel sessions: %w", err)
	}
	if len(raw) == 0 {
		return nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	for sessionID, session := range raw {
		historyJSON, err := json.Marshal(session.History)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO reel_sessions (id, history, position, last_activity)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (id) DO NOTHING
		`, strings.TrimSpace(sessionID), historyJSON, session.Position, session.LastActivity.UTC()); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

func (s *PostgresStore) readLegacyMemes() ([]Meme, error) {
	payload, err := os.ReadFile(filepath.Join(s.dataDir, "index.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read legacy index: %w", err)
	}
	if len(payload) == 0 {
		return nil, nil
	}

	var persisted []persistedMeme
	if err := json.Unmarshal(payload, &persisted); err != nil {
		return nil, fmt.Errorf("decode legacy index: %w", err)
	}

	memes := make([]Meme, 0, len(persisted))
	for _, item := range persisted {
		memes = append(memes, Meme{
			ID:            item.ID,
			OriginalName:  item.OriginalName,
			StoredName:    item.StoredName,
			FilePath:      item.FilePath,
			ContentType:   item.ContentType,
			SizeBytes:     item.SizeBytes,
			Tags:          normalizeTags(item.Tags),
			SuggestedTags: normalizeTags(item.SuggestedTags),
			Notes:         strings.TrimSpace(item.Notes),
			CreatedAt:     item.CreatedAt,
			UpdatedAt:     item.UpdatedAt,
		})
	}

	return memes, nil
}

func (s *PostgresStore) readLegacyFavorites() (map[string][]string, error) {
	payload, err := os.ReadFile(filepath.Join(s.dataDir, "favorites.json"))
	if errors.Is(err, os.ErrNotExist) {
		return map[string][]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read legacy favorites: %w", err)
	}
	if len(payload) == 0 {
		return map[string][]string{}, nil
	}

	var raw map[string][]string
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, fmt.Errorf("decode legacy favorites: %w", err)
	}
	if raw == nil {
		raw = map[string][]string{}
	}
	return raw, nil
}

type queryable interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

func (s *PostgresStore) getByID(ctx context.Context, db queryable, userID, id string) (Meme, error) {
	rows, err := db.Query(ctx, `
		SELECT
			m.id,
			m.original_name,
			m.stored_name,
			m.file_path,
			m.content_type,
			m.size_bytes,
			COALESCE(array_agg(t.name ORDER BY t.name) FILTER (WHERE t.name IS NOT NULL), '{}') AS tags,
			COALESCE(m.suggested_tags, '{}') AS suggested_tags,
			COALESCE(m.auto_suggest_disabled, FALSE) AS auto_suggest_disabled,
			m.notes,
			COALESCE(m.source_url, '') AS source_url,
			EXISTS (
				SELECT 1
				FROM user_favorites uf
				WHERE uf.user_id = $1 AND uf.meme_id = m.id
			) AS favorite,
			m.created_at,
			m.updated_at
		FROM memes m
		LEFT JOIN meme_tags mt ON mt.meme_id = m.id
		LEFT JOIN tags t ON t.id = mt.tag_id
		WHERE m.id = $2
			AND COALESCE(m.hidden_from_app, FALSE) = FALSE
		GROUP BY m.id
	`, userID, id)
	if err != nil {
		return Meme{}, err
	}
	defer rows.Close()

	if !rows.Next() {
		return Meme{}, os.ErrNotExist
	}

	meme, err := scanMemeRow(rows)
	if err != nil {
		return Meme{}, err
	}
	decoratePreviewPath(&meme, s.previewDir)
	return meme, nil
}

func (s *PostgresStore) replaceTags(ctx context.Context, tx pgx.Tx, memeID string, tags []string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM meme_tags WHERE meme_id = $1`, memeID); err != nil {
		return err
	}

	for _, tag := range normalizeTags(tags) {
		var tagID int64
		if err := tx.QueryRow(ctx, `
			INSERT INTO tags (name)
			VALUES ($1)
			ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
			RETURNING id
		`, tag).Scan(&tagID); err != nil {
			return err
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO meme_tags (meme_id, tag_id)
			VALUES ($1, $2)
			ON CONFLICT DO NOTHING
		`, memeID, tagID); err != nil {
			return err
		}
	}

	if _, err := tx.Exec(ctx, `
		DELETE FROM tags
		WHERE NOT EXISTS (
			SELECT 1
			FROM meme_tags mt
			WHERE mt.tag_id = tags.id
		)
	`); err != nil {
		return err
	}

	return nil
}

func (s *PostgresStore) setFavoriteInExecutor(ctx context.Context, db queryable, userID, memeID string, favorite bool) error {
	userID = normalizeFavoriteUserID(userID)

	var exists bool
	if err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM memes WHERE id = $1)`, memeID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return os.ErrNotExist
	}

	if favorite {
		_, err := db.Exec(ctx, `
			INSERT INTO user_favorites (user_id, meme_id)
			VALUES ($1, $2)
			ON CONFLICT DO NOTHING
		`, userID, memeID)
		return err
	}

	_, err := db.Exec(ctx, `DELETE FROM user_favorites WHERE user_id = $1 AND meme_id = $2`, userID, memeID)
	return err
}

func (s *PostgresStore) getByHash(ctx context.Context, contentHash string) (Meme, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT
			m.id,
			m.original_name,
			m.stored_name,
			m.file_path,
			m.content_type,
			m.size_bytes,
			COALESCE(array_agg(t.name ORDER BY t.name) FILTER (WHERE t.name IS NOT NULL), '{}') AS tags,
			COALESCE(m.suggested_tags, '{}') AS suggested_tags,
			COALESCE(m.auto_suggest_disabled, FALSE) AS auto_suggest_disabled,
			m.notes,
			COALESCE(m.source_url, '') AS source_url,
			FALSE AS favorite,
			m.created_at,
			m.updated_at
		FROM memes m
		LEFT JOIN meme_tags mt ON mt.meme_id = m.id
		LEFT JOIN tags t ON t.id = mt.tag_id
		WHERE m.content_hash = $1
		GROUP BY m.id
		LIMIT 1
	`, strings.TrimSpace(contentHash))
	if err != nil {
		return Meme{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		return Meme{}, os.ErrNotExist
	}
	meme, err := scanMemeRow(rows)
	if err != nil {
		return Meme{}, err
	}
	decoratePreviewPath(&meme, s.previewDir)
	return meme, nil
}

func (s *PostgresStore) ListMemeAudit(id string, limit int) ([]MemeAuditEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 100 {
		limit = 100
	}

	rows, err := s.pool.Query(context.Background(), `
		SELECT id, meme_id, action, actor_user_id, actor_username, actor_display_name, actor_avatar_url, description, created_at
		FROM meme_audit_logs
		WHERE meme_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2
	`, strings.TrimSpace(id), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []MemeAuditEntry{}
	for rows.Next() {
		var entry MemeAuditEntry
		if err := rows.Scan(
			&entry.ID,
			&entry.MemeID,
			&entry.Action,
			&entry.Actor.UserID,
			&entry.Actor.Username,
			&entry.Actor.DisplayName,
			&entry.Actor.AvatarURL,
			&entry.Description,
			&entry.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	return out, rows.Err()
}

func (s *PostgresStore) ListAuditFeed(offset int, limit int) (PagedAuditFeed, error) {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 250 {
		limit = 250
	}

	var total int
	if err := s.pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM meme_audit_logs`).Scan(&total); err != nil {
		return PagedAuditFeed{}, err
	}

	rows, err := s.pool.Query(context.Background(), `
		SELECT
			l.id,
			l.meme_id,
			l.action,
			l.actor_user_id,
			l.actor_username,
			l.actor_display_name,
			l.actor_avatar_url,
			l.description,
			l.created_at,
			COALESCE(m.original_name, '') AS meme_original_name,
			COALESCE(m.content_type, '') AS meme_content_type,
			COALESCE(m.file_path, '') AS meme_file_path
		FROM meme_audit_logs l
		LEFT JOIN memes m ON m.id = l.meme_id
		ORDER BY l.created_at DESC, l.id DESC
		OFFSET $1
		LIMIT $2
	`, offset, limit)
	if err != nil {
		return PagedAuditFeed{}, err
	}
	defer rows.Close()

	out := []GlobalMemeAuditEntry{}
	for rows.Next() {
		var entry GlobalMemeAuditEntry
		if err := rows.Scan(
			&entry.ID,
			&entry.MemeID,
			&entry.Action,
			&entry.Actor.UserID,
			&entry.Actor.Username,
			&entry.Actor.DisplayName,
			&entry.Actor.AvatarURL,
			&entry.Description,
			&entry.CreatedAt,
			&entry.MemeOriginalName,
			&entry.MemeContentType,
			&entry.MemeFilePath,
		); err != nil {
			return PagedAuditFeed{}, err
		}
		out = append(out, entry)
	}
	if err := rows.Err(); err != nil {
		return PagedAuditFeed{}, err
	}
	return PagedAuditFeed{
		Events:     out,
		Total:      total,
		HasMore:    offset+len(out) < total,
		NextOffset: min(offset+len(out), total),
	}, nil
}

func (s *PostgresStore) ListPendingDeletes(offset int, limit int) (PagedPendingDeletes, error) {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	var total int
	if err := s.pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM memes WHERE COALESCE(pending_delete, FALSE) = TRUE`).Scan(&total); err != nil {
		return PagedPendingDeletes{}, err
	}

	rows, err := s.pool.Query(context.Background(), `
		SELECT
			m.id,
			m.original_name,
			m.stored_name,
			m.file_path,
			m.content_type,
			m.size_bytes,
			COALESCE(array_agg(t.name ORDER BY t.name) FILTER (WHERE t.name IS NOT NULL), '{}') AS tags,
			COALESCE(m.suggested_tags, '{}') AS suggested_tags,
			COALESCE(m.auto_suggest_disabled, FALSE) AS auto_suggest_disabled,
			m.notes,
			COALESCE(m.source_url, '') AS source_url,
			FALSE AS favorite,
			m.created_at,
			m.updated_at,
			COALESCE(u.user_id, m.delete_requested_by_user_id) AS requested_by_user_id,
			COALESCE(u.username, '') AS requested_by_username,
			COALESCE(u.display_name, '') AS requested_by_display_name,
			COALESCE(u.avatar_url, '') AS requested_by_avatar_url,
			m.delete_requested_at
		FROM memes m
		LEFT JOIN meme_tags mt ON mt.meme_id = m.id
		LEFT JOIN tags t ON t.id = mt.tag_id
		LEFT JOIN app_users u ON u.user_id = m.delete_requested_by_user_id
		WHERE COALESCE(m.pending_delete, FALSE) = TRUE
		GROUP BY m.id, u.user_id, u.username, u.display_name, u.avatar_url, m.delete_requested_at
		ORDER BY m.delete_requested_at ASC NULLS LAST, m.created_at DESC
		OFFSET $1
		LIMIT $2
	`, offset, limit)
	if err != nil {
		return PagedPendingDeletes{}, err
	}
	defer rows.Close()

	out := []PendingDeleteRecord{}
	for rows.Next() {
		var record PendingDeleteRecord
		if err := rows.Scan(
			&record.Meme.ID,
			&record.Meme.OriginalName,
			&record.Meme.StoredName,
			&record.Meme.FilePath,
			&record.Meme.ContentType,
			&record.Meme.SizeBytes,
			&record.Meme.Tags,
			&record.Meme.Notes,
			&record.Meme.SourceURL,
			&record.Meme.Favorite,
			&record.Meme.CreatedAt,
			&record.Meme.UpdatedAt,
			&record.RequestedBy.UserID,
			&record.RequestedBy.Username,
			&record.RequestedBy.DisplayName,
			&record.RequestedBy.AvatarURL,
			&record.RequestedAt,
		); err != nil {
			return PagedPendingDeletes{}, err
		}
		decoratePreviewPath(&record.Meme, s.previewDir)
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return PagedPendingDeletes{}, err
	}
	return PagedPendingDeletes{
		Memes:      out,
		Total:      total,
		HasMore:    offset+len(out) < total,
		NextOffset: min(offset+len(out), total),
	}, nil
}

func (s *PostgresStore) ApprovePendingDelete(id string, actor AuditActor) error {
	ctx := context.Background()
	id = strings.TrimSpace(id)

	var storedName string
	err := s.pool.QueryRow(ctx, `SELECT stored_name FROM memes WHERE id = $1 AND COALESCE(pending_delete, FALSE) = TRUE`, id).Scan(&storedName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return os.ErrNotExist
		}
		return err
	}

	if err := s.insertAuditLog(ctx, s.pool, id, "delete_approved", actor, "Approved delete request"); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM memes WHERE id = $1`, id); err != nil {
		return err
	}

	targetPath := filepath.Join(s.uploadDir, storedName)
	if err := os.Remove(targetPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove upload file: %w", err)
	}
	thumbnailPath := filepath.Join(s.previewDir, thumbnailFileName(storedName))
	if err := os.Remove(thumbnailPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove thumbnail file: %w", err)
	}
	return nil
}

func (s *PostgresStore) RejectPendingDelete(id string, actor AuditActor) error {
	ctx := context.Background()
	commandTag, err := s.pool.Exec(ctx, `
		UPDATE memes
		SET hidden_from_app = FALSE,
			pending_delete = FALSE,
			delete_requested_by_user_id = '',
			delete_requested_at = NULL,
			updated_at = NOW()
		WHERE id = $1 AND COALESCE(pending_delete, FALSE) = TRUE
	`, strings.TrimSpace(id))
	if err != nil {
		return err
	}
	if commandTag.RowsAffected() == 0 {
		return os.ErrNotExist
	}
	return s.insertAuditLog(ctx, s.pool, strings.TrimSpace(id), "delete_rejected", actor, "Rejected delete request")
}

// RecordSystemAudit writes an audit entry that is not associated with a specific
// meme. The empty meme_id is surfaced by ListAuditFeed's LEFT JOIN as a row with
// no meme attached.
func (s *PostgresStore) RecordSystemAudit(action string, actor AuditActor, description string) error {
	return s.insertAuditLog(context.Background(), s.pool, "", action, actor, description)
}

func (s *PostgresStore) insertAuditLog(ctx context.Context, db queryable, memeID, action string, actor AuditActor, description string) error {
	_, err := db.Exec(ctx, `
		INSERT INTO meme_audit_logs (
			meme_id, action, actor_user_id, actor_username, actor_display_name, actor_avatar_url, description, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
	`, strings.TrimSpace(memeID), strings.TrimSpace(action), strings.TrimSpace(actor.UserID), strings.TrimSpace(actor.Username), strings.TrimSpace(actor.DisplayName), strings.TrimSpace(actor.AvatarURL), strings.TrimSpace(description))
	return err
}

func diffAuditTags(current []string, next []string) ([]string, []string) {
	currentSet := map[string]struct{}{}
	nextSet := map[string]struct{}{}
	for _, tag := range normalizeTags(current) {
		currentSet[tag] = struct{}{}
	}
	for _, tag := range normalizeTags(next) {
		nextSet[tag] = struct{}{}
	}

	added := []string{}
	removed := []string{}
	for tag := range nextSet {
		if _, ok := currentSet[tag]; !ok {
			added = append(added, tag)
		}
	}
	for tag := range currentSet {
		if _, ok := nextSet[tag]; !ok {
			removed = append(removed, tag)
		}
	}
	slices.Sort(added)
	slices.Sort(removed)
	return added, removed
}

func scanMemeRow(row interface{ Scan(dest ...any) error }) (Meme, error) {
	var meme Meme
	var tags []string
	var suggestedTags []string
	var autoSuggestDisabled bool
	if err := row.Scan(
		&meme.ID,
		&meme.OriginalName,
		&meme.StoredName,
		&meme.FilePath,
		&meme.ContentType,
		&meme.SizeBytes,
		&tags,
		&suggestedTags,
		&autoSuggestDisabled,
		&meme.Notes,
		&meme.SourceURL,
		&meme.Favorite,
		&meme.CreatedAt,
		&meme.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Meme{}, os.ErrNotExist
		}
		return Meme{}, err
	}
	// Tag names are written through normalizeTags and read back via
	// "array_agg(... ORDER BY t.name)", so they arrive lowercased, de-duplicated
	// (tags.name is UNIQUE), and sorted. Re-normalizing every row is wasted work.
	if tags == nil {
		tags = []string{}
	}
	meme.Tags = tags
	meme.SuggestedTags = normalizeTags(suggestedTags)
	meme.AutoSuggestDisabled = autoSuggestDisabled
	return meme, nil
}
