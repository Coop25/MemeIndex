package accessor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"
)

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

// UntaggedWithoutSuggestionIDs returns the ids of the memes counted by
// UntaggedWithoutSuggestionsCount, newest first.
func (s *PostgresStore) UntaggedWithoutSuggestionIDs() ([]string, error) {
	return scanIDList(context.Background(), s.pool, `
		SELECT m.id
		FROM memes m
		WHERE `+visibleMemePredicate+`
		  AND COALESCE(m.auto_suggest_disabled, FALSE) = FALSE
		  AND COALESCE(cardinality(m.suggested_tags), 0) = 0
		  AND NOT EXISTS (SELECT 1 FROM meme_tags mt WHERE mt.meme_id = m.id)
		ORDER BY m.created_at DESC, m.id ASC
	`)
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

// TagMergeCounts reports what a MergeTag moved: the memes that carried the
// source tag, the audiobooks that did, and the account audiobook-tag
// restrictions that pointed at it.
type TagMergeCounts struct {
	Memes        int
	Audiobooks   int
	Restrictions int
}

// auxTagLinkTables are the tables outside this package's own schema that
// reference the shared `tags` table with ON DELETE CASCADE. A merge must
// re-point them before the source tag is deleted, otherwise the cascade
// silently strips audiobook tags and - worse - an account's tag restrictions
// (an account with no restriction rows is unrestricted).
var auxTagLinkTables = []struct{ table, keyColumn string }{
	{"audiobook_tags", "audiobook_id"},
	{"app_user_audiobook_tag_restrictions", "user_id"},
}

// MergeTag repoints every meme_tags, audiobook_tags and
// app_user_audiobook_tag_restrictions link from sourceTag onto targetTag in a
// single transaction, creating targetTag if needed and deleting sourceTag. It
// returns how many of each were affected and writes the same tag_removed /
// tag_added audit trail the per-meme path produced.
func (s *PostgresStore) MergeTag(sourceTag, targetTag string, actor AuditActor) (TagMergeCounts, error) {
	ctx := context.Background()
	sourceTag = normalizeTag(sourceTag)
	targetTag = normalizeTag(targetTag)
	if sourceTag == "" || targetTag == "" {
		return TagMergeCounts{}, errors.New("source and target tags are required")
	}
	if sourceTag == targetTag {
		return TagMergeCounts{}, nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return TagMergeCounts{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var sourceID int64
	if err := tx.QueryRow(ctx, `SELECT id FROM tags WHERE name = $1`, sourceTag).Scan(&sourceID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return TagMergeCounts{}, nil
		}
		return TagMergeCounts{}, err
	}

	affected, err := scanIDList(ctx, tx, `SELECT meme_id FROM meme_tags WHERE tag_id = $1 ORDER BY meme_id`, sourceID)
	if err != nil {
		return TagMergeCounts{}, err
	}

	// Which of the audiobook-side link tables exist and still reference the
	// source tag. Counted before anything moves.
	type auxLink struct {
		table, keyColumn string
		count            int
	}
	var aux []auxLink
	auxTotal := 0
	for _, t := range auxTagLinkTables {
		exists, err := tableExists(ctx, tx, t.table)
		if err != nil {
			return TagMergeCounts{}, err
		}
		if !exists {
			continue
		}
		var n int
		if err := tx.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE tag_id = $1`, t.table), sourceID).Scan(&n); err != nil {
			return TagMergeCounts{}, err
		}
		aux = append(aux, auxLink{table: t.table, keyColumn: t.keyColumn, count: n})
		auxTotal += n
	}

	if len(affected) == 0 && auxTotal == 0 {
		if _, err := tx.Exec(ctx, `DELETE FROM tags WHERE id = $1`, sourceID); err != nil {
			return TagMergeCounts{}, err
		}
		return TagMergeCounts{}, tx.Commit(ctx)
	}

	var targetID int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO tags (name) VALUES ($1)
		ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
		RETURNING id
	`, targetTag).Scan(&targetID); err != nil {
		return TagMergeCounts{}, err
	}

	counts := TagMergeCounts{Memes: len(affected)}

	// Re-point the audiobook links before sourceTag is deleted: their tag_id
	// foreign keys cascade on delete.
	for _, l := range aux {
		if l.count == 0 {
			continue
		}
		if _, err := tx.Exec(ctx, fmt.Sprintf(`
			UPDATE %[1]s
			SET tag_id = $2
			WHERE tag_id = $1
			  AND NOT EXISTS (
				SELECT 1 FROM %[1]s existing
				WHERE existing.%[2]s = %[1]s.%[2]s AND existing.tag_id = $2
			  )
		`, l.table, l.keyColumn), sourceID, targetID); err != nil {
			return TagMergeCounts{}, err
		}
		// Rows whose key already had the target are redundant now.
		if _, err := tx.Exec(ctx, fmt.Sprintf(`DELETE FROM %s WHERE tag_id = $1`, l.table), sourceID); err != nil {
			return TagMergeCounts{}, err
		}
		if l.table == "audiobook_tags" {
			counts.Audiobooks = l.count
		} else {
			counts.Restrictions = l.count
		}
	}

	if len(affected) > 0 {
		alreadyTagged, err := scanIDList(ctx, tx, `SELECT meme_id FROM meme_tags WHERE tag_id = $1`, targetID)
		if err != nil {
			return TagMergeCounts{}, err
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
			return TagMergeCounts{}, err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM meme_tags WHERE tag_id = $1`, sourceID); err != nil {
			return TagMergeCounts{}, err
		}
		if _, err := tx.Exec(ctx, `UPDATE memes SET updated_at = NOW() WHERE id = ANY($1::text[])`, affected); err != nil {
			return TagMergeCounts{}, err
		}
		for _, id := range affected {
			if err := s.insertAuditLog(ctx, tx, id, "tag_removed", actor, fmt.Sprintf("Removed tag %q", sourceTag)); err != nil {
				return TagMergeCounts{}, err
			}
			if _, ok := hasTarget[id]; !ok {
				if err := s.insertAuditLog(ctx, tx, id, "tag_added", actor, fmt.Sprintf("Added tag %q", targetTag)); err != nil {
					return TagMergeCounts{}, err
				}
			}
		}
	}

	if _, err := tx.Exec(ctx, `DELETE FROM tags WHERE id = $1`, sourceID); err != nil {
		return TagMergeCounts{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return TagMergeCounts{}, err
	}
	return counts, nil
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

// replaceTags rewrites the tag set for one meme. It resolves the whole set in a
// fixed number of round trips - one batched tag upsert and one batched link
// insert - instead of two statements per tag, and it scopes the orphan-tag
// cleanup to just the tags this meme used to carry rather than re-sweeping the
// entire tags table on every upload and every edit.
func (s *PostgresStore) replaceTags(ctx context.Context, tx pgx.Tx, memeID string, tags []string) error {
	previousTagIDs, err := scanInt64List(ctx, tx, `SELECT tag_id FROM meme_tags WHERE meme_id = $1`, memeID)
	if err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `DELETE FROM meme_tags WHERE meme_id = $1`, memeID); err != nil {
		return err
	}

	names := normalizeTags(tags)
	if len(names) > 0 {
		if _, err := tx.Exec(ctx, `
			INSERT INTO tags (name)
			SELECT unnest($1::text[])
			ON CONFLICT (name) DO NOTHING
		`, names); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO meme_tags (meme_id, tag_id)
			SELECT $1, t.id FROM tags t WHERE t.name = ANY($2::text[])
			ON CONFLICT DO NOTHING
		`, memeID, names); err != nil {
			return err
		}
	}

	return s.deleteOrphanTags(ctx, tx, previousTagIDs)
}

// deleteOrphanTags removes any of the given tag ids that no longer have a single
// meme_tags link. It is the scoped replacement for the old whole-`tags`-table
// anti-join that ran on every write: callers pass just the ids that a delete or
// a tag-set rewrite might have orphaned.
func (s *PostgresStore) deleteOrphanTags(ctx context.Context, db queryable, tagIDs []int64) error {
	if len(tagIDs) == 0 {
		return nil
	}
	// audiobook_tags and app_user_audiobook_tag_restrictions tag into this
	// same shared `tags` table from the client package, so a tag a meme just
	// stopped using can still be in use by an audiobook, or by an account's
	// audiobook tag restriction, and must not be deleted out from under
	// either - checked here too, not just meme_tags. This package's own
	// schema never creates those two tables, so whether they exist is
	// checked at runtime (a plain query that names a nonexistent relation
	// fails before a conditional inside it is even evaluated - "IF EXISTS"
	// only works on DDL, not a row-level condition like this) rather than
	// assumed, so this package still works standalone - e.g. its own tests -
	// without hard-depending on client-package tables that may not be there.
	query := `DELETE FROM tags WHERE id = ANY($1::bigint[]) AND NOT EXISTS (SELECT 1 FROM meme_tags mt WHERE mt.tag_id = tags.id)`
	for _, table := range []string{"audiobook_tags", "app_user_audiobook_tag_restrictions"} {
		exists, err := tableExists(ctx, db, table)
		if err != nil {
			return err
		}
		if exists {
			query += fmt.Sprintf(` AND NOT EXISTS (SELECT 1 FROM %s WHERE tag_id = tags.id)`, table)
		}
	}
	_, err := db.Exec(ctx, query, tagIDs)
	return err
}
