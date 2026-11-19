package accessor

import (
	"context"
	"net/textproto"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// These tests hold one side of a tag-link race open in a transaction of the
// test's own, run the other side for real, and only then let the first side
// commit - the interleaving behind #80, where an orphan-tag delete cascaded
// away a link that a concurrent transaction had just committed.

// raceMeme creates a meme with unique content (Create refuses duplicates by
// content hash) carrying the given tags.
func (f *tagMergeFixture) raceMeme(tags ...string) string {
	f.t.Helper()
	actor := AuditActor{UserID: "u-race", Username: "race", IsSuperAdmin: true}
	token := randToken(f.t)
	m, err := f.store.Create(context.Background(), CreateInput{
		File:        strings.NewReader("tag race bytes " + token),
		Header:      textproto.MIMEHeader{"Content-Type": []string{"image/png"}},
		Filename:    f.token + "-" + token + ".png",
		ContentType: "image/png",
		Tags:        tags,
		Actor:       actor,
	})
	if err != nil {
		f.t.Fatalf("Create meme: %v", err)
	}
	f.t.Cleanup(func() { f.store.Delete(context.Background(), DeleteInput{ID: m.ID, Actor: actor}) })
	return m.ID
}

func (f *tagMergeFixture) memeTags(id string) []string {
	f.t.Helper()
	return f.names(`SELECT t.name FROM meme_tags mt JOIN tags t ON t.id = mt.tag_id WHERE mt.meme_id = $1 ORDER BY t.name`, id)
}

// backendPID returns the server process id behind tx, so the test can see
// when another session is waiting on one of its locks.
func backendPID(t *testing.T, tx pgx.Tx) int {
	t.Helper()
	var pid int
	if err := tx.QueryRow(context.Background(), `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatalf("pg_backend_pid: %v", err)
	}
	return pid
}

// waitBlockedOrDone returns once some session is waiting on a lock held by
// the backend blockerPID, or once done is closed - whichever comes first. A
// fixed sleep would make the interleaving a guess; this makes it certain that
// the other side has either finished or is parked behind the open transaction
// before that transaction commits.
func waitBlockedOrDone(t *testing.T, f *tagMergeFixture, blockerPID int, done <-chan struct{}) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-done:
			return
		default:
		}
		var blocked bool
		if err := f.store.pool.QueryRow(context.Background(), `
			SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE $1 = ANY(pg_blocking_pids(pid)))
		`, blockerPID).Scan(&blocked); err != nil {
			t.Fatalf("pg_blocking_pids: %v", err)
		}
		if blocked {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the concurrent side neither finished nor blocked on the open transaction")
}

// Removing the last use of a tag while another transaction is linking it to a
// different meme must not delete the tag and cascade that link away.
func TestOrphanTagCleanupKeepsConcurrentMemeLink(t *testing.T) {
	f := newTagMergeFixture(t)
	ctx := context.Background()
	tag := f.tag("kids")
	f.cleanupTags(tag)
	oldMeme := f.raceMeme(tag)
	newMeme := f.raceMeme()

	// The link side, left uncommitted: addMemeTags is what EditMeme runs.
	tx, err := f.store.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err := f.store.addMemeTags(ctx, tx, newMeme, []string{tag}); err != nil {
		t.Fatalf("addMemeTags: %v", err)
	}
	pid := backendPID(t, tx)

	// The orphan side, for real: drop the tag from the only meme that has it.
	done := make(chan struct{})
	var editErr error
	go func() {
		defer close(done)
		_, editErr = f.store.EditMeme(context.Background(), "", oldMeme, MemeEdit{TagsRemoved: []string{tag}, Actor: mergeActor})
	}()
	waitBlockedOrDone(t, f, pid, done)

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit link: %v", err)
	}
	<-done
	if editErr != nil {
		t.Fatalf("EditMeme: %v", editErr)
	}

	if got := f.memeTags(newMeme); !slices.Equal(got, []string{tag}) {
		t.Fatalf("linked meme tags = %v, want [%s]: the orphan cleanup cascaded the new link away", got, tag)
	}
	if got := f.memeTags(oldMeme); len(got) != 0 {
		t.Fatalf("edited meme tags = %v, want none", got)
	}
}

// The opposite order: an orphan cleanup has locked the tag and goes on to
// delete it while a meme edit is linking it. The edit has to notice the tag
// vanished and create it again rather than fail or link nothing.
func TestTagLinkRecreatesTagDeletedWhileWaiting(t *testing.T) {
	f := newTagMergeFixture(t)
	ctx := context.Background()
	tag := f.tag("gone")
	f.cleanupTags(tag)
	tagID := f.tagID(tag)
	meme := f.raceMeme()

	// The first half of DeleteOrphanTags: lock the candidate.
	tx, err := f.store.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT id FROM tags WHERE id = $1 FOR UPDATE`, tagID); err != nil {
		t.Fatalf("lock tag: %v", err)
	}
	pid := backendPID(t, tx)

	done := make(chan struct{})
	var editErr error
	go func() {
		defer close(done)
		_, editErr = f.store.EditMeme(context.Background(), "", meme, MemeEdit{TagsAdded: []string{tag}, Actor: mergeActor})
	}()
	waitBlockedOrDone(t, f, pid, done)

	// The second half: the tag is unused, so the cleanup deletes it.
	if _, err := tx.Exec(ctx, `DELETE FROM tags WHERE id = $1`, tagID); err != nil {
		t.Fatalf("delete tag: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit delete: %v", err)
	}
	<-done
	if editErr != nil {
		t.Fatalf("EditMeme: %v", editErr)
	}
	if got := f.memeTags(meme); !slices.Equal(got, []string{tag}) {
		t.Fatalf("meme tags = %v, want [%s]", got, tag)
	}
}

// A merge has to move a link that another transaction added to the source
// tag while the merge was starting, not delete the source out from under it.
func TestMergeTagMovesConcurrentlyAddedLink(t *testing.T) {
	f := newTagMergeFixture(t)
	ctx := context.Background()
	src, dst := f.tag("kidz"), f.tag("kids")
	f.cleanupTags(src, dst)
	oldMeme := f.raceMeme(src)
	newMeme := f.raceMeme()

	tx, err := f.store.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err := f.store.addMemeTags(ctx, tx, newMeme, []string{src}); err != nil {
		t.Fatalf("addMemeTags: %v", err)
	}
	pid := backendPID(t, tx)

	done := make(chan struct{})
	var counts TagMergeCounts
	var mergeErr error
	go func() {
		defer close(done)
		counts, mergeErr = f.store.MergeTag(context.Background(), src, dst, mergeActor)
	}()
	waitBlockedOrDone(t, f, pid, done)

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit link: %v", err)
	}
	<-done
	if mergeErr != nil {
		t.Fatalf("MergeTag: %v", mergeErr)
	}

	for _, id := range []string{oldMeme, newMeme} {
		if got := f.memeTags(id); !slices.Equal(got, []string{dst}) {
			t.Errorf("meme %s tags = %v, want [%s]", id, got, dst)
		}
	}
	if counts.Memes != 2 {
		t.Errorf("merged memes = %d, want 2", counts.Memes)
	}
	if f.tagExists(src) {
		t.Errorf("source tag %s still exists", src)
	}
}

// A merge into a tag that does not exist yet, while an edit of a meme that
// carries the source adds that same new tag. The edit holds the meme row and
// creates the tag; the merge has to lock that meme. If the merge created the
// target inside its own transaction first, the edit's upsert of the name would
// wait on the merge's uncommitted insert while the merge waits on the edit's
// meme row: a deadlock that Postgres resolves by failing one of the two.
func TestMergeTagIntoNewTagDoesNotDeadlockWithEditAddingIt(t *testing.T) {
	f := newTagMergeFixture(t)
	ctx := context.Background()
	src, dst := f.tag("catt"), f.tag("cats")
	f.cleanupTags(src, dst)
	meme := f.raceMeme(src)

	// The edit, held open: EditMeme locks the meme row first.
	tx, err := f.store.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT id FROM memes WHERE id = $1 FOR UPDATE`, meme); err != nil {
		t.Fatalf("lock meme: %v", err)
	}
	pid := backendPID(t, tx)

	done := make(chan struct{})
	var counts TagMergeCounts
	var mergeErr error
	go func() {
		defer close(done)
		counts, mergeErr = f.store.MergeTag(context.Background(), src, dst, mergeActor)
	}()
	waitBlockedOrDone(t, f, pid, done)

	// The rest of the edit: link the merge target, which is new.
	if err := f.store.addMemeTags(ctx, tx, meme, []string{dst}); err != nil {
		t.Fatalf("addMemeTags: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit edit: %v", err)
	}
	<-done
	if mergeErr != nil {
		t.Fatalf("MergeTag: %v", mergeErr)
	}

	if got := f.memeTags(meme); !slices.Equal(got, []string{dst}) {
		t.Errorf("meme tags = %v, want [%s]", got, dst)
	}
	if counts.Memes != 1 {
		t.Errorf("merged memes = %d, want 1", counts.Memes)
	}
	if f.tagExists(src) {
		t.Errorf("source tag %s still exists", src)
	}
}

// A merge into a new tag whose request is abandoned while it waits must not
// leave that tag behind. MergeTag commits the target before its own
// transaction starts locking, so the cleanup has to run past the cancelled
// request context; on the request's own context it would fail at once.
func TestCancelledMergeIntoNewTagLeavesNoTarget(t *testing.T) {
	f := newTagMergeFixture(t)
	ctx := context.Background()
	src, dst := f.tag("abandon"), f.tag("abandoned")
	f.cleanupTags(src, dst)
	meme := f.raceMeme(src)

	// Hold the meme row so the merge blocks after it has created the target.
	tx, err := f.store.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT id FROM memes WHERE id = $1 FOR UPDATE`, meme); err != nil {
		t.Fatalf("lock meme: %v", err)
	}
	pid := backendPID(t, tx)

	mergeCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	var mergeErr error
	go func() {
		defer close(done)
		_, mergeErr = f.store.MergeTag(mergeCtx, src, dst, mergeActor)
	}()
	waitBlockedOrDone(t, f, pid, done)
	if !f.tagExists(dst) {
		t.Fatalf("target tag %s was not created before the merge blocked", dst)
	}

	cancel() // the admin's browser goes away
	<-done
	if mergeErr == nil {
		t.Fatal("MergeTag succeeded although its context was cancelled while it waited")
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("release meme: %v", err)
	}

	if f.tagExists(dst) {
		t.Errorf("target tag %s was left behind by the abandoned merge", dst)
	}
	if got := f.memeTags(meme); !slices.Equal(got, []string{src}) {
		t.Errorf("meme tags = %v, want [%s] unchanged", got, src)
	}
}

// A merge of a source nothing links creates no target: the target the merge
// commits up front (see MergeTag) is cleaned up again when nothing moves.
func TestMergeTagOfUnusedSourceLeavesNoNewTarget(t *testing.T) {
	f := newTagMergeFixture(t)
	src, dst := f.tag("lonely"), f.tag("lonelier")
	f.cleanupTags(src, dst)
	f.tagID(src)

	if _, err := f.store.MergeTag(context.Background(), src, dst, mergeActor); err != nil {
		t.Fatalf("MergeTag: %v", err)
	}
	if f.tagExists(src) {
		t.Errorf("source tag %s still exists", src)
	}
	if f.tagExists(dst) {
		t.Errorf("target tag %s was left behind with nothing linking it", dst)
	}
}

// Merges queued on the merge lock must not starve the one holding it of a
// pool connection. Each waiter holds a connection while it waits, so a
// merge that needed a second connection once it had the lock (to commit a
// new target up front) stalled behind them until statement_timeout failed
// a waiter - with a pool no bigger than the merges queued, every time.
func TestConcurrentMergesDoNotExhaustThePool(t *testing.T) {
	f := newTagMergeFixture(t)
	ctx := context.Background()
	dsn := strings.TrimSpace(os.Getenv("MEMEINDEX_TEST_DATABASE_URL"))
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	small, err := NewPostgresStore(ctx, dsn+sep+"pool_max_conns=2&pool_min_conns=1&statement_timeout=5000", t.TempDir())
	if err != nil {
		t.Fatalf("NewPostgresStore: %v", err)
	}
	t.Cleanup(small.Close)

	pairs := [][2]string{{f.tag("qa"), f.tag("qa-new")}, {f.tag("qb"), f.tag("qb-new")}}
	for _, p := range pairs {
		f.cleanupTags(p[0], p[1])
		f.raceMeme(p[0])
	}

	// Hold the merge lock from outside so both merges queue behind it, each
	// on a connection of the two-connection pool.
	holder, err := f.store.pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Release()
	if _, err := holder.Exec(ctx, `SELECT pg_advisory_lock(hashtextextended('tag-merge', 0))`); err != nil {
		t.Fatalf("take merge lock: %v", err)
	}
	var holderPID int
	if err := holder.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&holderPID); err != nil {
		t.Fatal(err)
	}

	errs := make(chan error, len(pairs))
	for _, p := range pairs {
		go func() {
			_, err := small.MergeTag(p[0], p[1], mergeActor)
			errs <- err
		}()
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		var waiting int
		if err := f.store.pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM pg_stat_activity WHERE $1 = ANY(pg_blocking_pids(pid))
		`, holderPID).Scan(&waiting); err != nil {
			t.Fatalf("pg_blocking_pids: %v", err)
		}
		if waiting == len(pairs) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d of %d merges queued on the merge lock", waiting, len(pairs))
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := holder.Exec(ctx, `SELECT pg_advisory_unlock(hashtextextended('tag-merge', 0))`); err != nil {
		t.Fatalf("release merge lock: %v", err)
	}

	for range pairs {
		if err := <-errs; err != nil {
			t.Errorf("MergeTag: %v", err)
		}
	}
	for _, p := range pairs {
		if f.tagExists(p[0]) || !f.tagExists(p[1]) {
			t.Errorf("merge %s -> %s did not complete", p[0], p[1])
		}
	}
}
