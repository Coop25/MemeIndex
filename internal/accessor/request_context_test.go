package accessor

import (
	"context"
	"errors"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// These tests cover #78: store methods run their queries on the caller's
// context, so a request the client abandons stops its query instead of
// leaving it to run to the statement timeout, while the writes that touch
// files and rows together still finish.

func createContextTestMeme(t *testing.T, store *PostgresStore, ctx context.Context, token string) Meme {
	t.Helper()
	meme, err := store.Create(ctx, CreateInput{
		File:        strings.NewReader("request context bytes " + token),
		Header:      textproto.MIMEHeader{"Content-Type": []string{"image/png"}},
		Filename:    token + ".png",
		ContentType: "image/png",
		Tags:        []string{token + "-tag"},
		Actor:       AuditActor{UserID: "u-ctx", Username: "ctx"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		_, _ = store.Delete(context.Background(), DeleteInput{ID: meme.ID, Actor: AuditActor{UserID: "u-ctx", IsSuperAdmin: true}})
	})
	return meme
}

func cancelledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

// TestStoreQueryStopsWhenContextIsCancelled parks an edit behind a row lock
// the test holds, then cancels the edit's context. The edit must give up
// straight away with context.Canceled rather than wait for the lock.
func TestStoreQueryStopsWhenContextIsCancelled(t *testing.T) {
	store := newRegressionStore(t)
	token := "ctxslow" + randToken(t)
	meme := createContextTestMeme(t, store, context.Background(), token)

	blocker, err := store.pool.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin blocker: %v", err)
	}
	released := false
	release := func() {
		if !released {
			released = true
			_ = blocker.Rollback(context.Background())
		}
	}
	defer release()
	if _, err := blocker.Exec(context.Background(), `SELECT id FROM memes WHERE id = $1 FOR UPDATE`, meme.ID); err != nil {
		t.Fatalf("lock meme row: %v", err)
	}
	var blockerPID int
	if err := blocker.QueryRow(context.Background(), `SELECT pg_backend_pid()`).Scan(&blockerPID); err != nil {
		t.Fatalf("pg_backend_pid: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	notes := "should never be written"
	done := make(chan error, 1)
	go func() {
		_, err := store.EditMeme(ctx, "", meme.ID, MemeEdit{Notes: &notes})
		done <- err
	}()

	// Wait until the edit is really queued behind the lock, so the cancel
	// below interrupts a query in flight rather than racing its start.
	deadline := time.Now().Add(15 * time.Second)
	for {
		var blocked bool
		if err := store.pool.QueryRow(context.Background(), `
			SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE $1 = ANY(pg_blocking_pids(pid)))
		`, blockerPID).Scan(&blocked); err != nil {
			t.Fatalf("pg_blocking_pids: %v", err)
		}
		if blocked {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("EditMeme never waited on the held row lock")
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("EditMeme after cancel: err = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		release()
		err := <-done
		t.Fatalf("EditMeme kept waiting on the lock for 5s after its context was cancelled (returned %v once the lock was released)", err)
	}

	release()
	got, err := store.GetByID(context.Background(), "", meme.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Notes == notes {
		t.Fatal("cancelled edit was applied")
	}
}

// TestStoreMethodsUseTheCallerContext calls every cancellable store method
// with an already-cancelled context. Each one must refuse to run its query,
// which is only true if it passes that context to the database instead of
// one of its own.
func TestStoreMethodsUseTheCallerContext(t *testing.T) {
	store := newRegressionStore(t)
	token := "ctxuse" + randToken(t)
	meme := createContextTestMeme(t, store, context.Background(), token)
	actor := AuditActor{UserID: "u-ctx", Username: "ctx"}
	now := time.Now()
	notes := "cancelled"

	ctx := cancelledContext()
	calls := map[string]func() error{
		"GetByID":    func() error { _, err := store.GetByID(ctx, "", meme.ID); return err },
		"GetAnyByID": func() error { _, err := store.GetAnyByID(ctx, meme.ID); return err },
		"MemesByIDs": func() error { _, err := store.MemesByIDs(ctx, []string{meme.ID}); return err },
		"Random":     func() error { _, err := store.Random(ctx, nil); return err },
		"EditMeme": func() error {
			_, err := store.EditMeme(ctx, "", meme.ID, MemeEdit{Notes: &notes})
			return err
		},
		"SetFavorite": func() error { _, err := store.SetFavorite(ctx, "u-ctx", meme.ID, true); return err },
		"SetFavoriteWithActor": func() error {
			_, err := store.SetFavoriteWithActor(ctx, "u-ctx", meme.ID, true, actor)
			return err
		},
		"HiddenMemeIDs":       func() error { _, err := store.HiddenMemeIDs(ctx); return err },
		"AllStoredFiles":      func() error { _, err := store.AllStoredFiles(ctx); return err },
		"AdminDashboardMemes": func() error { _, err := store.AdminDashboardMemes(ctx); return err },
		"EnsurePreviewAssets": func() error { return store.EnsurePreviewAssets(ctx, nil) },
		"ListMemeAudit":       func() error { _, err := store.ListMemeAudit(ctx, meme.ID, 10); return err },
		"ListAuditFeed":       func() error { _, err := store.ListAuditFeed(ctx, 0, 10, ""); return err },
		"ListPendingDeletes":  func() error { _, err := store.ListPendingDeletes(ctx, 0, 10); return err },
		"RecordSystemAudit":   func() error { return store.RecordSystemAudit(ctx, "ctx_test", actor, "cancelled") },
		"CountAuditActionsByActor": func() error {
			_, err := store.CountAuditActionsByActor(ctx, "uploaded")
			return err
		},
		"CountFavoritesByUser":     func() error { _, err := store.CountFavoritesByUser(ctx); return err },
		"TotalFavoriteAssignments": func() error { _, err := store.TotalFavoriteAssignments(ctx); return err },
		"FavoriteActivitySince":    func() error { _, err := store.FavoriteActivitySince(ctx, now.Add(-time.Hour)); return err },
		"GetMemeShareState":        func() error { _, err := store.GetMemeShareState(ctx, meme.ID); return err },
		"ResolveMemeShareSlug":     func() error { _, _, err := store.ResolveMemeShareSlug(ctx, "nope"); return err },
		"ListActiveMemeShares":     func() error { _, err := store.ListActiveMemeShares(ctx, now); return err },
		"RevokeMemeShare":          func() error { return store.RevokeMemeShare(ctx, meme.ID) },
		"RevokeAllMemeShares":      func() error { _, err := store.RevokeAllMemeShares(ctx, now); return err },
		"LoadReelSessions":         func() error { _, err := store.LoadReelSessions(ctx); return err },
		"SaveReelSession": func() error {
			return store.SaveReelSession(ctx, token, ReelSessionRecord{LastActivity: now})
		},
		"DeleteReelSession":        func() error { return store.DeleteReelSession(ctx, token) },
		"CleanupStaleReelSessions": func() error { return store.CleanupStaleReelSessions(ctx, now.Add(-time.Hour)) },
		"TagUsageCounts":           func() error { _, err := store.TagUsageCounts(ctx); return err },
		"UntaggedWithoutSuggestionsCount": func() error {
			_, err := store.UntaggedWithoutSuggestionsCount(ctx)
			return err
		},
		"UntaggedWithoutSuggestionIDs": func() error { _, err := store.UntaggedWithoutSuggestionIDs(ctx); return err },
		"PendingSuggestionMemes":       func() error { _, _, err := store.PendingSuggestionMemes(ctx, 0, 10); return err },
		"MergeTag": func() error {
			_, err := store.MergeTag(ctx, token+"-tag", token+"-merged", actor)
			return err
		},
		"ListSuggestedTags":      func() error { _, err := store.ListSuggestedTags(ctx, meme.ID); return err },
		"ReplaceSuggestedTags":   func() error { return store.ReplaceSuggestedTags(ctx, meme.ID, []string{"x"}) },
		"RemoveSuggestedTag":     func() error { return store.RemoveSuggestedTag(ctx, meme.ID, "x") },
		"SetSearchText":          func() error { return store.SetSearchText(ctx, meme.ID, "x") },
		"SetAutoSuggestDisabled": func() error { return store.SetAutoSuggestDisabled(ctx, meme.ID, true) },
		"QueryMemes":             func() error { _, err := store.QueryMemes(ctx, MemeQuery{Limit: 10}); return err },
		"MemeDashboard":          func() error { _, err := store.MemeDashboard(ctx, "u-ctx"); return err },
		"TagCounts":              func() error { _, err := store.TagCounts(ctx, 10); return err },
	}
	for name, call := range calls {
		if err := call(); !errors.Is(err, context.Canceled) {
			t.Errorf("%s with a cancelled context: err = %v, want context.Canceled", name, err)
		}
	}

	// List and SuggestTags report no error; a cancelled one comes back empty.
	if got := store.List(ctx, "", token, false, ""); len(got) != 0 {
		t.Errorf("List with a cancelled context returned %d memes, want none", len(got))
	}
	if got := store.SuggestTags(ctx, token, 10); len(got) != 0 {
		t.Errorf("SuggestTags with a cancelled context returned %v, want none", got)
	}

	// Nothing the cancelled calls attempted may have landed.
	got, err := store.GetByID(context.Background(), "u-ctx", meme.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Notes == notes || got.Favorite || len(got.SuggestedTags) != 0 {
		t.Fatalf("a cancelled write was applied: %+v", got)
	}
}

// TestStoreFileWritesFinishDespiteCancelledContext checks the writes that
// touch a file and its row together. A request cancelled part-way through one
// of them would strand a file or a row, so they run to completion even when
// the caller's context is already cancelled.
func TestStoreFileWritesFinishDespiteCancelledContext(t *testing.T) {
	store := newRegressionStore(t)
	ctx := cancelledContext()
	admin := AuditActor{UserID: "u-ctx-admin", Username: "admin", IsSuperAdmin: true}
	member := AuditActor{UserID: "u-ctx-member", Username: "member"}

	uploadPath := func(m Meme) string { return filepath.Join(store.UploadDir(), m.StoredName) }
	assertGone := func(label string, m Meme) {
		t.Helper()
		if _, err := store.GetAnyByID(context.Background(), m.ID); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s: row still present (err %v)", label, err)
		}
		if _, err := os.Stat(uploadPath(m)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s: upload file still on disk (stat err %v)", label, err)
		}
	}

	// Create: the row and the file both exist afterwards.
	created := createContextTestMeme(t, store, ctx, "ctxcreate"+randToken(t))
	if _, err := store.GetByID(context.Background(), "", created.ID); err != nil {
		t.Fatalf("created meme not stored: %v", err)
	}
	if _, err := os.Stat(uploadPath(created)); err != nil {
		t.Fatalf("created meme's file missing: %v", err)
	}

	// A member's delete request, then its approval.
	approved := createContextTestMeme(t, store, context.Background(), "ctxapprove"+randToken(t))
	result, err := store.Delete(ctx, DeleteInput{ID: approved.ID, Actor: member})
	if err != nil || !result.PendingApproval {
		t.Fatalf("Delete request with a cancelled context = %+v, %v; want pending approval", result, err)
	}
	if err := store.ApprovePendingDelete(ctx, approved.ID, admin); err != nil {
		t.Fatalf("ApprovePendingDelete with a cancelled context: %v", err)
	}
	assertGone("approved delete", approved)

	// A member's delete request that is then rejected.
	rejected := createContextTestMeme(t, store, context.Background(), "ctxreject"+randToken(t))
	if _, err := store.Delete(context.Background(), DeleteInput{ID: rejected.ID, Actor: member}); err != nil {
		t.Fatalf("Delete request: %v", err)
	}
	if err := store.RejectPendingDelete(ctx, rejected.ID, admin); err != nil {
		t.Fatalf("RejectPendingDelete with a cancelled context: %v", err)
	}
	if _, err := store.GetByID(context.Background(), "", rejected.ID); err != nil {
		t.Errorf("rejected delete: meme still hidden (err %v)", err)
	}
	entries, err := store.ListMemeAudit(context.Background(), rejected.ID, 10)
	if err != nil || len(entries) == 0 || entries[0].Action != "delete_rejected" {
		t.Errorf("rejected delete: latest audit entry = %+v (err %v), want delete_rejected", entries, err)
	}

	// A super-admin's direct delete.
	deleted := createContextTestMeme(t, store, context.Background(), "ctxdelete"+randToken(t))
	result, err = store.Delete(ctx, DeleteInput{ID: deleted.ID, Actor: admin})
	if err != nil || !result.Deleted {
		t.Fatalf("Delete with a cancelled context = %+v, %v; want deleted", result, err)
	}
	assertGone("direct delete", deleted)
}

// TestShareCreationFinishesWhenCancelledBetweenItsWrites parks the second of
// GetOrCreateMemeShare's two writes (the link insert) behind a conflicting
// insert the test holds open, then cancels the request. The first write (the
// meme's new share generation and expiry) has committed by then, so giving up
// would leave the meme listed as actively shared with no link. The share must
// be finished instead.
func TestShareCreationFinishesWhenCancelledBetweenItsWrites(t *testing.T) {
	store := newRegressionStore(t)
	meme := createContextTestMeme(t, store, context.Background(), "ctxshare"+randToken(t))

	var generation int64
	if err := store.pool.QueryRow(context.Background(), `SELECT share_generation FROM memes WHERE id = $1`, meme.ID).Scan(&generation); err != nil {
		t.Fatalf("read share generation: %v", err)
	}
	// Never shared, so the share about to be made takes the next generation.
	// An uncommitted link for that generation makes its insert wait.
	blocker, err := store.pool.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin blocker: %v", err)
	}
	released := false
	release := func() {
		if !released {
			released = true
			_ = blocker.Rollback(context.Background())
		}
	}
	defer release()
	if _, err := blocker.Exec(context.Background(), `
		INSERT INTO meme_share_links (slug, meme_id, generation) VALUES ($1, $2, $3)
	`, "blocker-"+randToken(t), meme.ID, generation+1); err != nil {
		t.Fatalf("hold link insert: %v", err)
	}
	var blockerPID int
	if err := blocker.QueryRow(context.Background(), `SELECT pg_backend_pid()`).Scan(&blockerPID); err != nil {
		t.Fatalf("pg_backend_pid: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Now()
	type outcome struct {
		share MemeShareState
		err   error
	}
	done := make(chan outcome, 1)
	go func() {
		share, err := store.GetOrCreateMemeShare(ctx, meme.ID, "u-ctx", now, now.Add(time.Hour))
		done <- outcome{share, err}
	}()

	deadline := time.Now().Add(15 * time.Second)
	for {
		var blocked bool
		if err := store.pool.QueryRow(context.Background(), `
			SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE $1 = ANY(pg_blocking_pids(pid)))
		`, blockerPID).Scan(&blocked); err != nil {
			t.Fatalf("pg_blocking_pids: %v", err)
		}
		if blocked {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("GetOrCreateMemeShare never waited on the held link insert")
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	// Give a cancellable insert time to give up before the way clears.
	time.Sleep(200 * time.Millisecond)
	release()

	var got outcome
	select {
	case got = <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("GetOrCreateMemeShare did not return after the held insert was rolled back")
	}
	if got.err != nil {
		t.Fatalf("GetOrCreateMemeShare cancelled between its writes: %v", got.err)
	}
	if got.share.Slug == "" || got.share.Generation != generation+1 {
		t.Fatalf("share = %+v, want a link for generation %d", got.share, generation+1)
	}
	state, err := store.GetMemeShareState(context.Background(), meme.ID)
	if err != nil {
		t.Fatalf("GetMemeShareState: %v", err)
	}
	if state.Slug != got.share.Slug {
		t.Fatalf("stored share link = %q, want %q: the meme is shared with no link for its generation", state.Slug, got.share.Slug)
	}
}
