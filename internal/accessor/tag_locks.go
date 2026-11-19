package accessor

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// The `tags` table is shared by three link tables - meme_tags,
// audiobook_tags and app_user_audiobook_tag_restrictions - and all of them
// reference it with ON DELETE CASCADE. Deleting a tag row that looks unused
// is therefore only safe if nobody can be linking it at the same moment: a
// link another transaction has inserted but not yet committed is invisible to
// the delete's NOT EXISTS checks, and the cascade would then remove that link
// as soon as it commits. Losing an account's only audiobook tag restriction
// that way leaves the account unrestricted, so this fails open.
//
// Every path that links or deletes tags follows one row-lock protocol on the
// `tags` rows, under READ COMMITTED:
//
//   - Linking (LockTagsForLink) upserts the names, then takes FOR KEY SHARE
//     on the tag rows, in id order, before the transaction writes any link
//     row. Key-share locks never conflict with each other, so linkers never
//     wait on one another; they wait only for a transaction that holds the
//     row exclusively because it is deleting it.
//   - Deleting orphans (DeleteOrphanTags) takes FOR UPDATE SKIP LOCKED on the
//     candidates and only then re-checks every link table, in a separate
//     statement whose snapshot is taken after the lock. A candidate some
//     linker holds is skipped: it is about to be in use (or, if that linker
//     rolls back, it just stays as an unused tag row until something next
//     orphans it). A linker that arrives after the lock waits, finds the row
//     gone, and creates it afresh.
//   - Merging (MergeTag) runs one at a time and takes the source row FOR
//     UPDATE, waiting for linkers already in flight, after it has locked
//     the target the way a linker does.
//
// This cannot deadlock on tag rows. Orphan deletes never wait for a tag lock
// (SKIP LOCKED), so they can never close a wait cycle. Linkers only take
// shared locks, all in one statement in id order before they touch any link
// row, so the only tag lock a linker waits for is an exclusive one: held by
// an orphan delete, which never waits and so finishes, or by the one merge
// that can run at a time. That merge takes its source lock before it touches
// any meme or link row, then locks the memes that carry the source. A linker
// waiting for the source holds at most the row of the meme it is editing,
// and that meme does not carry the source (it would not be adding it
// otherwise), so the merge never needs anything the linker holds. Separately
// from row locks, an upsert of a brand-new tag name waits for any other
// transaction inserting the same name; that wait can only complete a cycle
// if a merge, an edit of a meme carrying the merge source and another edit
// that both links the source and creates the same new tag all meet at once.
// Postgres would detect such a deadlock and fail one of the requests; no link
// would be lost.

// maxTagLockAttempts bounds how often LockTagsForLink re-creates tags that a
// concurrent orphan delete removed between its upsert and its lock. Each retry
// needs another delete of the same tag to commit in that window, so in
// practice the second attempt always succeeds.
const maxTagLockAttempts = 5

// LockTagsForLink makes sure every named tag exists and takes FOR KEY SHARE on
// their rows for the rest of tx, returning their ids in ascending order. Call
// it before writing any link row (meme_tags, audiobook_tags,
// app_user_audiobook_tag_restrictions), and insert the links by the returned
// ids: while the lock is held no orphan-tag cleanup can delete those rows, so
// the links cannot be cascaded away. tx must be READ COMMITTED (the default),
// so that a retry's upsert sees a delete that committed while it waited.
func LockTagsForLink(ctx context.Context, tx pgx.Tx, tags []string) ([]int64, error) {
	names := normalizeTags(tags)
	if len(names) == 0 {
		return nil, nil
	}
	for attempt := 0; attempt < maxTagLockAttempts; attempt++ {
		if _, err := tx.Exec(ctx, `
			INSERT INTO tags (name)
			SELECT unnest($1::text[])
			ON CONFLICT (name) DO NOTHING
		`, names); err != nil {
			return nil, fmt.Errorf("ensure tags exist: %w", err)
		}
		// A row an orphan delete is removing makes this wait for it; once it
		// commits the row is gone and is simply missing from the result, so
		// the loop upserts it again.
		ids, err := scanInt64List(ctx, tx, `
			SELECT id FROM tags
			WHERE name = ANY($1::text[])
			ORDER BY id
			FOR KEY SHARE
		`, names)
		if err != nil {
			return nil, fmt.Errorf("lock tags: %w", err)
		}
		if len(ids) == len(names) {
			return ids, nil
		}
	}
	return nil, fmt.Errorf("lock tags: tags were deleted concurrently %d times in a row", maxTagLockAttempts)
}

// DeleteOrphanTags deletes those of the given tag ids that no meme, audiobook
// or account audiobook tag restriction links any more. It is the scoped
// replacement for a whole-`tags`-table anti-join: callers pass just the ids a
// delete or a tag-set rewrite might have orphaned. It must run inside a
// READ COMMITTED transaction, because the lock it takes has to be held while
// the second statement re-checks the link tables with a fresh snapshot.
func DeleteOrphanTags(ctx context.Context, tx pgx.Tx, tagIDs []int64) error {
	if len(tagIDs) == 0 {
		return nil
	}
	// Lock first. Rows a linker holds (FOR KEY SHARE) are skipped rather than
	// waited for: they are being linked, so they are not orphans. Waiting
	// instead would also let two transactions that each link one tag and
	// orphan the other deadlock.
	locked, err := scanInt64List(ctx, tx, `
		SELECT id FROM tags
		WHERE id = ANY($1::bigint[])
		ORDER BY id
		FOR UPDATE SKIP LOCKED
	`, tagIDs)
	if err != nil {
		return fmt.Errorf("lock orphan tag candidates: %w", err)
	}
	if len(locked) == 0 {
		return nil
	}
	// audiobook_tags and app_user_audiobook_tag_restrictions tag into this
	// same shared `tags` table from the client and audiobooks packages, so a
	// tag a meme just stopped using can still be in use by an audiobook, or
	// by an account's audiobook tag restriction, and must not be deleted out
	// from under either. This package's own schema never creates those two
	// tables, so whether they exist is checked at runtime (a plain query that
	// names a nonexistent relation fails before a conditional inside it is
	// even evaluated - "IF EXISTS" only works on DDL, not a row-level
	// condition like this) rather than assumed, so this package still works
	// standalone - e.g. its own tests - without hard-depending on tables that
	// may not be there.
	//
	// This is a new statement, so under READ COMMITTED it sees every link
	// committed by a linker the lock above had to get past.
	query := `DELETE FROM tags WHERE id = ANY($1::bigint[]) AND NOT EXISTS (SELECT 1 FROM meme_tags mt WHERE mt.tag_id = tags.id)`
	for _, table := range []string{"audiobook_tags", "app_user_audiobook_tag_restrictions"} {
		exists, err := tableExists(ctx, tx, table)
		if err != nil {
			return err
		}
		if exists {
			query += fmt.Sprintf(` AND NOT EXISTS (SELECT 1 FROM %s WHERE tag_id = tags.id)`, table)
		}
	}
	if _, err := tx.Exec(ctx, query, locked); err != nil {
		return fmt.Errorf("delete orphan tags: %w", err)
	}
	return nil
}
