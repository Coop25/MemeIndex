package accessor

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/textproto"
	"os"
	"strings"
	"testing"
)

// TestPostgresStoreIntegration exercises the real SQL query paths - create,
// paginated query with facet counts, free-text search, tag update, favorite
// toggle, and delete - against a live database.
//
// It is skipped unless MEMEINDEX_TEST_DATABASE_URL points at a disposable
// Postgres. CI provides one via a service container; locally you can run:
//
//	docker run --rm -e POSTGRES_PASSWORD=pg -p 5432:5432 postgres:17
//	MEMEINDEX_TEST_DATABASE_URL='postgres://postgres:pg@localhost:5432/postgres?sslmode=disable' \
//	  go test ./internal/accessor -run TestPostgresStoreIntegration -v
//
// Every row it creates carries a unique token in its notes/filename, so it is
// safe to run repeatedly against the same database without cleanup.
func TestPostgresStoreIntegration(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("MEMEINDEX_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("set MEMEINDEX_TEST_DATABASE_URL to run the Postgres integration test")
	}

	ctx := context.Background()
	store, err := NewPostgresStore(ctx, dsn, t.TempDir())
	if err != nil {
		t.Fatalf("NewPostgresStore: %v", err)
	}
	t.Cleanup(store.Close)

	if !store.SearchIndexAvailable() {
		t.Error("SearchIndexAvailable() = false; pg_trgm should be creatable on the stock postgres image")
	}

	token := "itest" + randToken(t)
	// IsSuperAdmin so Delete hard-deletes (the non-admin path only flags a row
	// for approval), letting the test exercise the real cascade and clean up.
	actor := AuditActor{UserID: "u-int", Username: "integration", IsSuperAdmin: true}

	img, err := store.Create(CreateInput{
		File:        strings.NewReader("fake png bytes"),
		Header:      textproto.MIMEHeader{"Content-Type": []string{"image/png"}},
		Filename:    token + "-pic.png",
		ContentType: "image/png",
		Notes:       "an " + token + " picture",
		Tags:        []string{token + "-alpha"},
		Actor:       actor,
	})
	if err != nil {
		t.Fatalf("Create image: %v", err)
	}
	vid, err := store.Create(CreateInput{
		File:        strings.NewReader("fake mp4 bytes"),
		Header:      textproto.MIMEHeader{"Content-Type": []string{"video/mp4"}},
		Filename:    token + "-clip.mp4",
		ContentType: "video/mp4",
		Notes:       "a " + token + " video",
		Actor:       actor,
	})
	if err != nil {
		t.Fatalf("Create video: %v", err)
	}

	page, err := store.QueryMemes(MemeQuery{Search: token, Limit: 50})
	if err != nil {
		t.Fatalf("QueryMemes: %v", err)
	}
	if page.Counts.Total != 2 || page.Counts.Images != 1 || page.Counts.Videos != 1 {
		t.Fatalf("facet counts = %+v, want Total=2 Images=1 Videos=1", page.Counts)
	}
	if page.Counts.Untagged != 1 {
		t.Fatalf("Untagged count = %d, want 1 (the video has no tags)", page.Counts.Untagged)
	}
	if len(page.Memes) != 2 {
		t.Fatalf("QueryMemes returned %d memes, want 2", len(page.Memes))
	}

	// Free-text search on a notes-only term.
	onlyVideo, err := store.QueryMemes(MemeQuery{Search: token + " video", Limit: 50})
	if err != nil {
		t.Fatalf("QueryMemes(search video): %v", err)
	}
	if len(onlyVideo.Memes) != 1 || onlyVideo.Memes[0].ID != vid.ID {
		t.Fatalf("notes search matched %d rows, want just the video", len(onlyVideo.Memes))
	}

	// Tag update is reflected in GetByID and in the untagged facet. Notes are
	// passed through unchanged (Update overwrites the notes column).
	if _, err := store.Update(actor.UserID, vid.ID, MemeUpdate{
		Tags:  []string{token + "-beta"},
		Notes: "a " + token + " video",
		Actor: actor,
	}); err != nil {
		t.Fatalf("Update tags: %v", err)
	}
	got, err := store.GetByID(actor.UserID, vid.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if len(got.Tags) != 1 || got.Tags[0] != token+"-beta" {
		t.Fatalf("updated tags = %v", got.Tags)
	}
	if page, err := store.QueryMemes(MemeQuery{Search: token, Limit: 50}); err != nil {
		t.Fatalf("QueryMemes after tag update: %v", err)
	} else if page.Counts.Untagged != 0 {
		t.Fatalf("Untagged count after tagging both = %d, want 0", page.Counts.Untagged)
	}

	// Favorite toggle + favorites-only filter.
	if _, err := store.SetFavorite(actor.UserID, img.ID, true); err != nil {
		t.Fatalf("SetFavorite: %v", err)
	}
	favs, err := store.QueryMemes(MemeQuery{UserID: actor.UserID, Search: token, FavoritesOnly: true, Limit: 50})
	if err != nil {
		t.Fatalf("QueryMemes(favorites): %v", err)
	}
	if len(favs.Memes) != 1 || favs.Memes[0].ID != img.ID {
		t.Fatalf("favorites-only returned %d rows, want just the image", len(favs.Memes))
	}

	// Delete removes it from subsequent queries.
	if _, err := store.Delete(DeleteInput{ID: img.ID, Actor: actor}); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	after, err := store.QueryMemes(MemeQuery{Search: token, Limit: 50})
	if err != nil {
		t.Fatalf("QueryMemes after delete: %v", err)
	}
	if after.Counts.Total != 1 || len(after.Memes) != 1 || after.Memes[0].ID != vid.ID {
		t.Fatalf("after delete: counts=%+v memes=%d", after.Counts, len(after.Memes))
	}

	// Leave the database as we found it.
	if _, err := store.Delete(DeleteInput{ID: vid.ID, Actor: actor}); err != nil {
		t.Fatalf("cleanup Delete: %v", err)
	}
}

func randToken(t *testing.T) string {
	t.Helper()
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}
