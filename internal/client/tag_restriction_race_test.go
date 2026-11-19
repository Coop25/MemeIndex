package client

import (
	"context"
	"net/textproto"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"memeindex/internal/accessor"
)

// An admin restricting an account to one audiobook tag at the same moment a
// meme edit drops that tag's last other use (#80). The edit's orphan-tag
// cleanup must not delete the tag and cascade the new restriction away: an
// account with no restriction rows is unrestricted, so losing it would
// silently open every audiobook to the account.
func TestOrphanTagCleanupKeepsConcurrentRestriction(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("MEMEINDEX_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("set MEMEINDEX_TEST_DATABASE_URL to run the Postgres integration test")
	}
	ctx := context.Background()

	// The meme store first: it creates the shared tags table the
	// restriction table references.
	memes, err := accessor.NewPostgresStore(ctx, dsn, t.TempDir())
	if err != nil {
		t.Fatalf("NewPostgresStore: %v", err)
	}
	t.Cleanup(memes.Close)
	authStore, err := newAuthUserStore(ctx, dsn)
	if err != nil {
		t.Fatalf("newAuthUserStore: %v", err)
	}
	users := authStore.(*postgresAuthUserStore)
	t.Cleanup(users.Close)

	token := "restrict" + randomHex(t)
	tag := token + "-kids"
	userID := token + "-user"
	t.Cleanup(func() {
		_ = users.DeleteUser(context.Background(), userID)
		_, _ = users.pool.Exec(context.Background(), `DELETE FROM tags WHERE name = $1`, tag)
	})
	if _, err := users.CreateUser(ctx, userID); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	actor := accessor.AuditActor{UserID: "admin", Username: "admin", IsSuperAdmin: true}
	meme, err := memes.Create(accessor.CreateInput{
		File:        strings.NewReader("restriction race bytes " + token),
		Header:      textproto.MIMEHeader{"Content-Type": []string{"image/png"}},
		Filename:    token + ".png",
		ContentType: "image/png",
		Tags:        []string{tag},
		Actor:       actor,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _, _ = memes.Delete(accessor.DeleteInput{ID: meme.ID, Actor: actor}) })

	// The restriction, written but not yet committed.
	tx, err := users.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err := setUserAudiobookTagRestrictionTx(ctx, tx, userID, []string{tag}); err != nil {
		t.Fatalf("setUserAudiobookTagRestrictionTx: %v", err)
	}
	var pid int
	if err := tx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatal(err)
	}

	// The meme edit that orphans the tag, for real.
	done := make(chan struct{})
	var editErr error
	go func() {
		defer close(done)
		_, editErr = memes.EditMeme("", meme.ID, accessor.MemeEdit{TagsRemoved: []string{tag}, Actor: actor})
	}()

	// Commit the restriction only once the edit has finished or is parked
	// behind it, so the race is the one from the issue every run.
	deadline := time.Now().Add(15 * time.Second)
	for waiting := true; waiting; {
		select {
		case <-done:
			waiting = false
			continue
		default:
		}
		var blocked bool
		if err := users.pool.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE $1 = ANY(pg_blocking_pids(pid)))
		`, pid).Scan(&blocked); err != nil {
			t.Fatalf("pg_blocking_pids: %v", err)
		}
		if blocked {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the meme edit neither finished nor blocked on the restriction")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit restriction: %v", err)
	}
	<-done
	if editErr != nil {
		t.Fatalf("EditMeme: %v", editErr)
	}

	record, ok, err := users.GetUser(ctx, userID)
	if err != nil || !ok {
		t.Fatalf("GetUser: ok=%v err=%v", ok, err)
	}
	if got := record.Permissions.AudiobookTagRestriction; !slices.Equal(got, []string{tag}) {
		t.Fatalf("restriction = %v, want [%s]: the account lost its only restriction and is now unrestricted", got, tag)
	}
}
