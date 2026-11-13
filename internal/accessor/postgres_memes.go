package accessor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/sync/errgroup"
)

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
				SELECT 1
				FROM user_favorites uf
				WHERE uf.user_id = $1 AND uf.meme_id = m.id
			) AS favorite,
			m.created_at,
			m.updated_at
		FROM memes m
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
		ORDER BY m.created_at DESC, m.id ASC
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

// adminDashboardMemeColumns mirrors memeRowSelectColumns for scanMemeRow but
// hard-codes the favourite flag to FALSE. The admin dashboard aggregates
// favourite totals from user_favorites directly (AdminAnalyticsStore), so the
// per-row EXISTS subquery that List() runs for the whole archive is dead weight
// here. It takes no positional args.
const adminDashboardMemeColumns = `
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
	FALSE AS favorite,
	m.created_at,
	m.updated_at`

// AdminDashboardMemes returns every visible meme, newest first, in the shape the
// admin overview aggregates - List() without the search/tag filters and without
// the per-row favourites lookup.
func (s *PostgresStore) AdminDashboardMemes() ([]Meme, error) {
	ctx := context.Background()
	rows, err := s.pool.Query(ctx, "SELECT"+adminDashboardMemeColumns+`
		FROM memes m
		WHERE COALESCE(m.hidden_from_app, FALSE) = FALSE
		ORDER BY m.created_at DESC, m.id ASC
	`)
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
	return memes, rows.Err()
}

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

	pageWhere := where
	if vc := memeViewCondition(q.View, 1); vc != "" {
		pageWhere += "\n  AND " + vc
	}
	if q.FavoritesOnly {
		pageWhere += "\n  AND EXISTS (SELECT 1 FROM user_favorites uf WHERE uf.user_id = $1 AND uf.meme_id = m.id)"
	}

	// The facet counts depend only on the search + tag filter, so they are the
	// same for every scroll page of one filter. Resolve them for the first page
	// only; later pages keep the counts the client already holds. When they are
	// needed, run them concurrently with the row page on a separate pooled
	// connection instead of back to back.
	wantCounts := offset == 0

	var counts MemeCategoryCounts
	var memes []Meme
	g, gctx := errgroup.WithContext(ctx)
	if wantCounts {
		countArgs := append([]any{uid}, filterArgs...)
		g.Go(func() error {
			c, err := s.memeCounts(gctx, where, countArgs)
			if err != nil {
				return err
			}
			counts = c
			return nil
		})
	}
	g.Go(func() error {
		// Fetch one extra row to decide HasMore without a second query.
		rows, err := s.queryMemeRows(gctx, uid, pageWhere, memeSortOrder(q.Sort), limit+1, offset, filterArgs...)
		if err != nil {
			return err
		}
		memes = rows
		return nil
	})
	if err := g.Wait(); err != nil {
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
		rows, err := s.queryMemeRows(ctx, uid, visible, "m.created_at DESC, m.id ASC", DashboardRecentLimit, 0)
		data.RecentItems = rows
		return err
	})
	g.Go(func() error {
		rows, err := s.queryMemeRows(ctx, uid,
			visible+" AND EXISTS (SELECT 1 FROM user_favorites uf WHERE uf.user_id = $1 AND uf.meme_id = m.id)",
			"m.created_at DESC, m.id ASC", DashboardFavoriteLimit, 0,
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
		tags, err := s.TagCounts(DashboardTopTagLimit)
		data.TopTags = tags
		return err
	})

	if err := g.Wait(); err != nil {
		return MemeDashboardData{}, err
	}
	return data, nil
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

// MemesByIDs resolves a batch of visible memes in one round trip, keyed by id.
// Ids that do not match a visible meme are simply absent from the result, the
// same outcome a per-id GetByID("", id) lookup would produce.
func (s *PostgresStore) MemesByIDs(ids []string) (map[string]Meme, error) {
	out := make(map[string]Meme, len(ids))
	clean := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		trimmed := strings.TrimSpace(id)
		if trimmed == "" {
			continue
		}
		if _, dup := seen[trimmed]; dup {
			continue
		}
		seen[trimmed] = struct{}{}
		clean = append(clean, trimmed)
	}
	if len(clean) == 0 {
		return out, nil
	}

	rows, err := s.pool.Query(context.Background(), "SELECT"+memeRowSelectColumns+`
		FROM memes m
		WHERE m.id = ANY($2::text[])
		  AND COALESCE(m.hidden_from_app, FALSE) = FALSE
	`, normalizeFavoriteUserID(""), clean)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		meme, scanErr := scanMemeRow(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		decoratePreviewPath(&meme, s.previewDir)
		out[meme.ID] = meme
	}
	return out, rows.Err()
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

	ctx := context.Background()

	// Check for a duplicate before generating a thumbnail: an ffmpeg run for a
	// file that is about to be rejected is wasted work, and its thumbnail used
	// to be left behind on disk.
	if existing, err := s.getByHash(ctx, meme.ContentHash); err == nil {
		_ = os.Remove(targetPath)
		return Meme{}, s.duplicateError(ctx, existing)
	} else if !errors.Is(err, os.ErrNotExist) {
		_ = os.Remove(targetPath)
		return Meme{}, err
	}

	// Thumbnail generation is best-effort.
	_ = ensurePreviewAsset(s.uploadDir, s.previewDir, &meme)
	cleanup := func() { removeUploadAndThumbnail(s.uploadDir, s.previewDir, storedName) }

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		cleanup()
		return Meme{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		INSERT INTO memes (
			id, original_name, stored_name, file_path, content_type, content_hash, size_bytes, notes, source_url, suggested_tags, auto_suggest_disabled, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`, meme.ID, meme.OriginalName, meme.StoredName, meme.FilePath, meme.ContentType, meme.ContentHash, meme.SizeBytes, meme.Notes, meme.SourceURL, meme.SuggestedTags, meme.AutoSuggestDisabled, meme.CreatedAt, meme.UpdatedAt); err != nil {
		cleanup()
		if isUniqueViolation(err) {
			if existing, lookupErr := s.getByHash(ctx, meme.ContentHash); lookupErr == nil {
				return Meme{}, s.duplicateError(ctx, existing)
			}
		}
		return Meme{}, err
	}

	if err := s.replaceTags(ctx, tx, meme.ID, meme.Tags); err != nil {
		cleanup()
		return Meme{}, err
	}

	if err := s.insertAuditLog(ctx, tx, meme.ID, "uploaded", input.Actor, fmt.Sprintf("Uploaded %s", meme.OriginalName)); err != nil {
		cleanup()
		return Meme{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		cleanup()
		return Meme{}, err
	}

	return meme, nil
}

// duplicateError builds the error for an upload whose content already exists.
// When that existing copy is hidden pending delete approval it reports so,
// instead of pointing the uploader at a meme they cannot open.
func (s *PostgresStore) duplicateError(ctx context.Context, existing Meme) error {
	var hidden bool
	if err := s.pool.QueryRow(ctx, `SELECT COALESCE(hidden_from_app, FALSE) FROM memes WHERE id = $1`, existing.ID).Scan(&hidden); err == nil && hidden {
		return &DuplicateMemeError{PendingDelete: true}
	}
	return &DuplicateMemeError{Existing: existing}
}

// isUniqueViolation reports whether err is, or wraps, a Postgres
// unique-constraint violation (SQLSTATE 23505).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func (s *PostgresStore) Update(userID, id string, update MemeUpdate) (Meme, error) {
	ctx := context.Background()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Meme{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

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
	defer func() { _ = tx.Rollback(ctx) }()

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

// HiddenMemeIDs lists the memes hidden from the app pending delete approval.
func (s *PostgresStore) HiddenMemeIDs() ([]string, error) {
	return scanIDList(context.Background(), s.pool, `SELECT id FROM memes WHERE COALESCE(hidden_from_app, FALSE) = TRUE`)
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

	// Capture the meme's tags before the cascade drops its meme_tags rows so any
	// that are left unreferenced can be pruned (replaceTags no longer sweeps the
	// whole tags table, so each removal path cleans up after itself).
	orphanCandidates, err := scanInt64List(ctx, s.pool, `SELECT tag_id FROM meme_tags WHERE meme_id = $1`, id)
	if err != nil {
		return DeleteResult{}, err
	}

	commandTag, err := s.pool.Exec(ctx, `DELETE FROM memes WHERE id = $1`, id)
	if err != nil {
		return DeleteResult{}, err
	}
	if commandTag.RowsAffected() == 0 {
		return DeleteResult{}, os.ErrNotExist
	}

	if err := s.deleteOrphanTags(ctx, s.pool, orphanCandidates); err != nil {
		return DeleteResult{}, err
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
	// (tags.name is UNIQUE), and sorted. suggested_tags is likewise only ever
	// written through normalizeTags. Re-normalizing either on every row is wasted
	// work; just guarantee a non-nil slice.
	if tags == nil {
		tags = []string{}
	}
	if suggestedTags == nil {
		suggestedTags = []string{}
	}
	meme.Tags = tags
	meme.SuggestedTags = suggestedTags
	meme.AutoSuggestDisabled = autoSuggestDisabled
	return meme, nil
}

// CountFavoritesByUser returns how many memes each user has favorited.
func (s *PostgresStore) CountFavoritesByUser() (map[string]int, error) {
	return s.countByUser(`SELECT user_id, COUNT(*) FROM user_favorites GROUP BY user_id`)
}
