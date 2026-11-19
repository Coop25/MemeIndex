package accessor

import (
	"context"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// isolatedDatabaseURL returns a DSN bound to a fresh, empty schema, so the
// store under test sees an empty memes table the way a brand-new database
// would. Skips unless MEMEINDEX_TEST_DATABASE_URL is set.
func isolatedDatabaseURL(t *testing.T) string {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("MEMEINDEX_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("set MEMEINDEX_TEST_DATABASE_URL to run the Postgres integration test")
	}
	ctx := context.Background()
	schema := "legacyjson_" + randToken(t)
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		admin.Close()
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		admin.Close()
	})
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	q := u.Query()
	q.Set("search_path", schema+",public")
	u.RawQuery = q.Encode()
	return u.String()
}

// An instance that ran on the local JSON store upgrades by pointing
// MEMEINDEX_DATABASE_URL at an empty database. Its memes, tags, notes, source
// links, favorites and reel sessions must all come across, and the uploaded
// files must stay where they are.
func TestLegacyJSONStoreUpgradesIntoEmptyPostgres(t *testing.T) {
	dsn := isolatedDatabaseURL(t)
	dataDir := t.TempDir()

	legacy := newLegacyJSONDataDir(t, dataDir)
	cat := legacy.create(t, CreateInput{
		File:        strings.NewReader("legacy cat bytes"),
		Header:      textproto.MIMEHeader{"Content-Type": []string{"image/png"}},
		Filename:    "cat.png",
		ContentType: "image/png",
		Tags:        []string{"cat", "reaction"},
		Notes:       "the classic",
		SourceURL:   "https://example.com/cat",
	})
	dog := legacy.create(t, CreateInput{
		File:        strings.NewReader("legacy dog bytes"),
		Header:      textproto.MIMEHeader{"Content-Type": []string{"video/mp4"}},
		Filename:    "dog.mp4",
		ContentType: "video/mp4",
		Tags:        []string{"dog"},
	})
	legacy.setFavorite(t, "discord-user-1", cat.ID)
	legacy.setAutoSuggestDisabled(t, dog.ID, true)
	now := time.Now().UTC().Truncate(time.Second)
	share := legacy.share(t, cat.ID, "discord-user-1", now, now.Add(365*24*time.Hour))
	if share.Slug == "" || share.Generation <= 0 {
		t.Fatalf("legacy share = %+v, want a slug and a generation", share)
	}
	reelPayload := `{"reel-1":{"History":["` + cat.ID + `","` + dog.ID + `"],"Position":1,"LastActivity":"` +
		time.Now().UTC().Format(time.RFC3339) + `"}}`
	if err := os.WriteFile(filepath.Join(dataDir, "reel_sessions.json"), []byte(reelPayload), 0o644); err != nil {
		t.Fatalf("write reel sessions: %v", err)
	}

	store, err := NewPostgresStore(context.Background(), dsn, dataDir)
	if err != nil {
		t.Fatalf("NewPostgresStore: %v", err)
	}
	t.Cleanup(store.Close)

	got, err := store.GetByID("discord-user-1", cat.ID)
	if err != nil {
		t.Fatalf("GetByID cat: %v", err)
	}
	if !got.Favorite {
		t.Error("cat lost its favorite")
	}
	if !slices.Equal(got.Tags, []string{"cat", "reaction"}) {
		t.Errorf("cat tags = %v", got.Tags)
	}
	if got.Notes != "the classic" || got.SourceURL != "https://example.com/cat" {
		t.Errorf("cat notes/source = %q / %q", got.Notes, got.SourceURL)
	}
	if got.StoredName != cat.StoredName {
		t.Errorf("cat stored name = %q, want %q", got.StoredName, cat.StoredName)
	}
	var hash string
	var dogAutoSuggestDisabled bool
	if err := store.pool.QueryRow(context.Background(), `SELECT content_hash FROM memes WHERE id = $1`, cat.ID).Scan(&hash); err != nil {
		t.Fatalf("read content hash: %v", err)
	}
	if hash == "" || hash != cat.ContentHash {
		t.Errorf("cat content hash = %q, want %q (duplicate detection depends on it)", hash, cat.ContentHash)
	}
	if err := store.pool.QueryRow(context.Background(), `SELECT auto_suggest_disabled FROM memes WHERE id = $1`, dog.ID).Scan(&dogAutoSuggestDisabled); err != nil {
		t.Fatalf("read auto_suggest_disabled: %v", err)
	}
	if !dogAutoSuggestDisabled {
		t.Error("dog lost auto_suggest_disabled")
	}

	// A share link the JSON-store instance already handed out keeps working:
	// same slug, same generation (which the share token's HMAC covers), same
	// expiry.
	memeID, generation, err := store.ResolveMemeShareSlug(share.Slug)
	if err != nil || memeID != cat.ID || generation != share.Generation {
		t.Errorf("ResolveMemeShareSlug = %q/%d err %v, want %q/%d", memeID, generation, err, cat.ID, share.Generation)
	}
	state, err := store.GetMemeShareState(cat.ID)
	if err != nil {
		t.Fatalf("GetMemeShareState: %v", err)
	}
	if state.Generation != share.Generation || !state.ExpiresAt.Equal(share.ExpiresAt) || state.Slug != share.Slug || state.SharedByUserID != "discord-user-1" {
		t.Errorf("share state = %+v, want %+v", state, share)
	}
	if _, err := os.Stat(filepath.Join(store.UploadDir(), got.StoredName)); err != nil {
		t.Errorf("cat upload not found in place: %v", err)
	}

	page, err := store.QueryMemes(context.Background(), MemeQuery{UserID: "discord-user-1", Search: "tag:dog", Limit: 10})
	if err != nil {
		t.Fatalf("QueryMemes: %v", err)
	}
	if len(page.Memes) != 1 || page.Memes[0].ID != dog.ID {
		t.Errorf("tag:dog search = %+v, want only the dog", page.Memes)
	}

	sessions, err := store.LoadReelSessions()
	if err != nil {
		t.Fatalf("LoadReelSessions: %v", err)
	}
	if s, ok := sessions["reel-1"]; !ok || s.Position != 1 || len(s.History) != 2 {
		t.Errorf("reel session = %+v (found %v), want position 1 with 2 entries", s, ok)
	}

	// A restart must not import again or duplicate anything.
	store.Close()
	again, err := NewPostgresStore(context.Background(), dsn, dataDir)
	if err != nil {
		t.Fatalf("second NewPostgresStore: %v", err)
	}
	t.Cleanup(again.Close)
	all, err := again.QueryMemes(context.Background(), MemeQuery{UserID: "discord-user-1", Limit: 10})
	if err != nil {
		t.Fatalf("QueryMemes after restart: %v", err)
	}
	if all.Counts.Total != 2 {
		t.Errorf("total after restart = %d, want 2", all.Counts.Total)
	}
}
