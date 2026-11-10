package accessor

import (
	"context"
	"errors"
	"net/textproto"
	"os"
	"strings"
	"testing"
	"time"
)

// TestPostgresShareLinksIntegration runs the share link SQL against a live
// database: issuing a link, renewing it, replacing it after expiry and
// revocation, and resolving current and historical codes. It is skipped
// unless MEMEINDEX_TEST_DATABASE_URL is set (see TestPostgresStoreIntegration).
func TestPostgresShareLinksIntegration(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("MEMEINDEX_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("set MEMEINDEX_TEST_DATABASE_URL to run the Postgres integration test")
	}
	store, err := NewPostgresStore(context.Background(), dsn, t.TempDir())
	if err != nil {
		t.Fatalf("NewPostgresStore: %v", err)
	}
	t.Cleanup(store.Close)

	actor := AuditActor{UserID: "u-share", Username: "integration", IsSuperAdmin: true}
	meme, err := store.Create(CreateInput{
		File:        strings.NewReader("share link bytes " + randToken(t)),
		Header:      textproto.MIMEHeader{"Content-Type": []string{"image/png"}},
		Filename:    "share-" + randToken(t) + ".png",
		ContentType: "image/png",
		Actor:       actor,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _, _ = store.Delete(DeleteInput{ID: meme.ID, Actor: actor}) })

	if state, err := store.GetMemeShareState(meme.ID); err != nil || state.Slug != "" {
		t.Fatalf("unshared meme state = %+v, err = %v", state, err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	first, err := store.GetOrCreateMemeShare(meme.ID, "user-1", now, now.Add(time.Hour))
	if err != nil {
		t.Fatalf("GetOrCreateMemeShare: %v", err)
	}
	if len(first.Slug) != 36 || first.Generation != 1 || first.SharedByUserID != "user-1" {
		t.Fatalf("first share = %+v", first)
	}

	// Renewal keeps the generation, link and sharer; only the expiry moves.
	renewed, err := store.GetOrCreateMemeShare(meme.ID, "user-2", now.Add(time.Minute), now.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("renew: %v", err)
	}
	if renewed.Slug != first.Slug || renewed.Generation != 1 || renewed.SharedByUserID != "user-1" || !renewed.ExpiresAt.Equal(now.Add(2*time.Hour)) {
		t.Fatalf("renewed share = %+v, want same link with a later expiry", renewed)
	}

	state, err := store.GetMemeShareState(meme.ID)
	if err != nil || state.Slug != first.Slug {
		t.Fatalf("GetMemeShareState = %+v, err = %v", state, err)
	}
	active, err := store.ListActiveMemeShares(now)
	if err != nil {
		t.Fatalf("ListActiveMemeShares: %v", err)
	}
	listed := false
	for _, share := range active {
		if share.MemeID == meme.ID {
			listed = share.Slug == first.Slug
		}
	}
	if !listed {
		t.Fatal("active share list is missing the share or its link code")
	}

	if memeID, generation, err := store.ResolveMemeShareSlug(first.Slug); err != nil || memeID != meme.ID || generation != 1 {
		t.Fatalf("ResolveMemeShareSlug = %q, %d, %v", memeID, generation, err)
	}
	if _, _, err := store.ResolveMemeShareSlug("00000000-0000-4000-8000-000000000000"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unknown code err = %v, want os.ErrNotExist", err)
	}

	// Revoke, then share again: a new generation and link, with the old code
	// still resolving to the meme under its old generation.
	if err := store.RevokeMemeShare(meme.ID); err != nil {
		t.Fatalf("RevokeMemeShare: %v", err)
	}
	if state, err := store.GetMemeShareState(meme.ID); err != nil || state.Slug != "" || state.Generation != 2 {
		t.Fatalf("revoked state = %+v, err = %v", state, err)
	}
	second, err := store.GetOrCreateMemeShare(meme.ID, "user-2", now, now.Add(time.Hour))
	if err != nil {
		t.Fatalf("share after revoke: %v", err)
	}
	if second.Slug == first.Slug || len(second.Slug) != 36 || second.Generation != 3 || second.SharedByUserID != "user-2" {
		t.Fatalf("second share = %+v", second)
	}
	if memeID, generation, err := store.ResolveMemeShareSlug(first.Slug); err != nil || memeID != meme.ID || generation != 1 {
		t.Fatalf("old code after re-share = %q, %d, %v", memeID, generation, err)
	}

	// A share that has expired is replaced, not renewed.
	third, err := store.GetOrCreateMemeShare(meme.ID, "user-3", now.Add(2*time.Hour), now.Add(3*time.Hour))
	if err != nil {
		t.Fatalf("share after expiry: %v", err)
	}
	if third.Slug == second.Slug || third.Generation != 4 {
		t.Fatalf("share after expiry = %+v, want a new link", third)
	}

	// Deleting the meme removes its link codes.
	if _, err := store.Delete(DeleteInput{ID: meme.ID, Actor: actor}); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, _, err := store.ResolveMemeShareSlug(third.Slug); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("code of a deleted meme err = %v, want os.ErrNotExist", err)
	}
}
