package accessor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"
)

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

	// Merges run one at a time, so two of them can never wait on each
	// other's tag locks (a merge of a into b next to one of b into a).
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('tag-merge', 0))`); err != nil {
		return TagMergeCounts{}, err
	}

	var sourceExists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM tags WHERE name = $1)`, sourceTag).Scan(&sourceExists); err != nil {
		return TagMergeCounts{}, err
	}
	if !sourceExists {
		return TagMergeCounts{}, nil
	}

	// Create a missing target in a statement of its own, committed before
	// this transaction takes any row lock. Created inside it, the new row
	// would stay uncommitted until the merge ends, and an edit that adds the
	// same name to a meme carrying the source - holding that meme's row,
	// which the merge locks below - would wait on the insert while the merge
	// waits on the meme: a deadlock. If the merge then fails or moves
	// nothing, the target it created is cleaned up again.
	var createdTargetID int64
	err = s.pool.QueryRow(ctx, `INSERT INTO tags (name) VALUES ($1) ON CONFLICT (name) DO NOTHING RETURNING id`, targetTag).Scan(&createdTargetID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) { // no row: it already existed
		return TagMergeCounts{}, err
	}
	committed := false
	defer func() {
		if !committed && createdTargetID != 0 {
			// Roll back first, so the cleanup is not skipped over this
			// transaction's own lock on the target.
			_ = tx.Rollback(ctx)
			_ = s.deleteOrphanTagsInTx(ctx, []int64{createdTargetID})
		}
	}()

	// The target is locked like any linker locks a tag before the source is
	// locked.
	targetIDs, err := LockTagsForLink(ctx, tx, []string{targetTag})
	if err != nil {
		return TagMergeCounts{}, err
	}
	targetID := targetIDs[0]

	// Lock the source exclusively. This waits for any transaction that is
	// linking it right now, and makes every later one wait for this merge
	// (and then re-create the tag), so the links read below are all of them
	// and none can be added before the source row is deleted. Without the
	// lock, a link committed after these reads would be cascaded away by the
	// DELETE at the end instead of being moved to the target.
	var sourceID int64
	if err := tx.QueryRow(ctx, `SELECT id FROM tags WHERE name = $1 FOR UPDATE`, sourceTag).Scan(&sourceID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Deleted while this waited; a target this created is
			// cleaned up again.
			return TagMergeCounts{}, nil
		}
		return TagMergeCounts{}, err
	}

	affected, err := scanIDList(ctx, tx, `SELECT meme_id FROM meme_tags WHERE tag_id = $1 ORDER BY meme_id`, sourceID)
	if err != nil {
		return TagMergeCounts{}, err
	}
	if len(affected) > 0 {
		// Take the affected memes' row locks, in id order, before touching
		// their meme_tags rows - the same order a meme edit takes them in
		// (meme row, then its tags) - so an edit removing the source tag from
		// one of them cannot deadlock with this merge. Nothing can add the
		// source to another meme meanwhile, but an edit this waited on may
		// have removed it, so read the list again under the locks.
		if _, err := tx.Exec(ctx, `SELECT id FROM memes WHERE id = ANY($1::text[]) ORDER BY id FOR UPDATE`, affected); err != nil {
			return TagMergeCounts{}, err
		}
		affected, err = scanIDList(ctx, tx, `SELECT meme_id FROM meme_tags WHERE tag_id = $1 ORDER BY meme_id`, sourceID)
		if err != nil {
			return TagMergeCounts{}, err
		}
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
		// Nothing moved onto the target, so do not leave it behind unused
		// if it was created above.
		if err := DeleteOrphanTags(ctx, tx, targetIDs); err != nil {
			return TagMergeCounts{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return TagMergeCounts{}, err
		}
		committed = true
		return TagMergeCounts{}, nil
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
	committed = true
	return counts, nil
}

// tableExists reports whether a relation is present in the database. Used for
// tables owned by other packages' schemas (audiobooks), which may not exist
// when this package runs standalone.
func tableExists(ctx context.Context, db queryable, name string) (bool, error) {
	var exists bool
	if err := db.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, name).Scan(&exists); err != nil {
		return false, fmt.Errorf("check %s exists: %w", name, err)
	}
	return exists, nil
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

// RemoveSuggestedTag drops one tag from a meme's stored suggestions with
// array_remove, so it works on whatever list is stored when it runs rather
// than on a copy the caller read earlier. updated_at only moves when the tag
// was actually there.
func (s *PostgresStore) RemoveSuggestedTag(id, tag string) error {
	commandTag, err := s.pool.Exec(context.Background(), `
		UPDATE memes
		SET suggested_tags = array_remove(suggested_tags, $2::text),
			updated_at = CASE WHEN $2::text = ANY(suggested_tags) THEN NOW() ELSE updated_at END
		WHERE id = $1
	`, strings.TrimSpace(id), normalizeTag(tag))
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
// fixed number of round trips - one batched tag upsert-and-lock and one
// batched link insert - instead of two statements per tag, and it scopes the
// orphan-tag cleanup to just the tags this meme used to carry rather than
// re-sweeping the entire tags table on every upload. Edits go through
// addMemeTags and removeMemeTags instead, which touch only the tags that
// change. The new tags are locked before any meme_tags row is touched (see
// LockTagsForLink).
func (s *PostgresStore) replaceTags(ctx context.Context, tx pgx.Tx, memeID string, tags []string) error {
	tagIDs, err := LockTagsForLink(ctx, tx, tags)
	if err != nil {
		return err
	}

	previousTagIDs, err := scanInt64List(ctx, tx, `SELECT tag_id FROM meme_tags WHERE meme_id = $1`, memeID)
	if err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `DELETE FROM meme_tags WHERE meme_id = $1`, memeID); err != nil {
		return err
	}

	if err := linkMemeTagIDs(ctx, tx, memeID, tagIDs); err != nil {
		return err
	}

	return DeleteOrphanTags(ctx, tx, previousTagIDs)
}

// addMemeTags links just the named tags to one meme, creating any tag that
// does not exist yet, and leaves the meme's other tags alone.
func (s *PostgresStore) addMemeTags(ctx context.Context, tx pgx.Tx, memeID string, tags []string) error {
	tagIDs, err := LockTagsForLink(ctx, tx, tags)
	if err != nil {
		return err
	}
	return linkMemeTagIDs(ctx, tx, memeID, tagIDs)
}

// linkMemeTagIDs inserts meme_tags rows for tag ids that LockTagsForLink has
// locked in this transaction.
func linkMemeTagIDs(ctx context.Context, tx pgx.Tx, memeID string, tagIDs []int64) error {
	if len(tagIDs) == 0 {
		return nil
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO meme_tags (meme_id, tag_id)
		SELECT $1, unnest($2::bigint[])
		ON CONFLICT DO NOTHING
	`, memeID, tagIDs)
	return err
}

// removeMemeTags unlinks just the named tags from one meme, then deletes any
// of them that nothing uses any more.
func (s *PostgresStore) removeMemeTags(ctx context.Context, tx pgx.Tx, memeID string, tags []string) error {
	names := normalizeTags(tags)
	if len(names) == 0 {
		return nil
	}
	removedIDs, err := scanInt64List(ctx, tx, `
		DELETE FROM meme_tags mt
		USING tags t
		WHERE mt.meme_id = $1 AND mt.tag_id = t.id AND t.name = ANY($2::text[])
		RETURNING mt.tag_id
	`, memeID, names)
	if err != nil {
		return err
	}
	return DeleteOrphanTags(ctx, tx, removedIDs)
}

// deleteOrphanTagsInTx runs DeleteOrphanTags in a transaction of its own, for
// the meme-delete paths that otherwise work statement by statement on the
// pool: its lock has to outlive the statement that takes it.
func (s *PostgresStore) deleteOrphanTagsInTx(ctx context.Context, tagIDs []int64) error {
	if len(tagIDs) == 0 {
		return nil
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		return DeleteOrphanTags(ctx, tx, tagIDs)
	})
}
