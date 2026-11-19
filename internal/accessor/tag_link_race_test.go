package accessor

import (
	"context"
	"net/textproto"
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
	m, err := f.store.Create(CreateInput{
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
	f.t.Cleanup(func() { f.store.Delete(DeleteInput{ID: m.ID, Actor: actor}) })
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
		_, editErr = f.store.EditMeme("", oldMeme, MemeEdit{TagsRemoved: []string{tag}, Actor: mergeActor})
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
		_, editErr = f.store.EditMeme("", meme, MemeEdit{TagsAdded: []string{tag}, Actor: mergeActor})
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
		counts, mergeErr = f.store.MergeTag(src, dst, mergeActor)
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
