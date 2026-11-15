package accessor

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
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
// favourite totals from user_favorites directly (FavoriteAnalyticsStore), so the
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
func (s *PostgresStore) QueryMemes(ctx context.Context, q MemeQuery) (MemeQueryPage, error) {
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

// TagCounts returns the most-used tags across visible memes, most frequent
// first, ties broken by name.
func (s *PostgresStore) TagCounts(ctx context.Context, limit int) ([]TagCount, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.pool.Query(ctx, `
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

const visibleMemePredicate = "COALESCE(m.hidden_from_app, FALSE) = FALSE"

// MemeDashboard resolves the user home screen summary in Postgres instead of
// scanning the whole archive into memory. The seven independent reads are run
// concurrently on separate pooled connections; the first error cancels the rest.
func (s *PostgresStore) MemeDashboard(ctx context.Context, userID string) (MemeDashboardData, error) {
	uid := normalizeFavoriteUserID(userID)
	const visible = "COALESCE(m.hidden_from_app, FALSE) = FALSE"

	var data MemeDashboardData
	g, ctx := errgroup.WithContext(ctx)

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
		tags, err := s.TagCounts(ctx, DashboardTopTagLimit)
		data.TopTags = tags
		return err
	})

	if err := g.Wait(); err != nil {
		return MemeDashboardData{}, err
	}
	return data, nil
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
