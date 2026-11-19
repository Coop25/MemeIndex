package accessor

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// legacyJSONDataDir writes a data directory the way the JSON-file MemeStore
// left it before MemeIndex became Postgres-only: uploads/<id><ext>,
// index.json and favorites.json. PostgresStore imports that layout once, into
// an empty database, so these files are what an upgrading instance still has
// on disk. Each mutation rewrites the file the old store rewrote for it, in
// the same encoding (see TestLegacyJSONDataDirMatchesJSONStoreFormat).
type legacyJSONDataDir struct {
	dir       string
	memes     []Meme
	favorites map[string]map[string]struct{}
}

func newLegacyJSONDataDir(t *testing.T, dir string) *legacyJSONDataDir {
	t.Helper()
	// The JSON store created thumbnails/ as soon as it previewed an upload;
	// these fixture uploads are too small to have produced a thumbnail in it.
	for _, sub := range []string{"uploads", "thumbnails"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatalf("create legacy %s dir: %v", sub, err)
		}
	}
	return &legacyJSONDataDir{dir: dir, favorites: map[string]map[string]struct{}{}}
}

// create stores an upload and its index entry as the JSON store's Create did.
func (d *legacyJSONDataDir) create(t *testing.T, input CreateInput) Meme {
	t.Helper()
	id := uuid.Must(uuid.NewV6()).String()
	storedName := id + filepath.Ext(input.Filename)
	data, err := io.ReadAll(input.File)
	if err != nil {
		t.Fatalf("read legacy upload: %v", err)
	}
	if err := os.WriteFile(filepath.Join(d.dir, "uploads", storedName), data, 0o644); err != nil {
		t.Fatalf("write legacy upload: %v", err)
	}
	hasher := newContentHashWriter()
	_, _ = hasher.Write(data)
	now := time.Now().UTC()
	meme := Meme{
		ID:           id,
		OriginalName: input.Filename,
		StoredName:   storedName,
		FilePath:     "/uploads/" + storedName,
		ContentType:  detectContentType(input.Header, input.ContentType, input.Filename),
		ContentHash:  contentHashString(hasher),
		SizeBytes:    int64(len(data)),
		Tags:         normalizeTags(input.Tags),
		Notes:        strings.TrimSpace(input.Notes),
		SourceURL:    strings.TrimSpace(input.SourceURL),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	d.memes = append(d.memes, meme)
	d.writeIndex(t)
	return meme
}

func (d *legacyJSONDataDir) setFavorite(t *testing.T, userID, memeID string) {
	t.Helper()
	userID = normalizeFavoriteUserID(userID)
	if d.favorites[userID] == nil {
		d.favorites[userID] = map[string]struct{}{}
	}
	d.favorites[userID][memeID] = struct{}{}
	d.writeFavorites(t)
}

func (d *legacyJSONDataDir) setAutoSuggestDisabled(t *testing.T, memeID string, disabled bool) {
	t.Helper()
	meme := d.meme(t, memeID)
	meme.AutoSuggestDisabled = disabled
	meme.UpdatedAt = time.Now().UTC()
	d.writeIndex(t)
}

// share issues or renews a share link as the JSON store's
// GetOrCreateMemeShare did: an expired or never-shared meme moves to the next
// generation and gets a fresh link code; an active share is only extended.
func (d *legacyJSONDataDir) share(t *testing.T, memeID, userID string, now, expiresAt time.Time) MemeShareState {
	t.Helper()
	meme := d.meme(t, memeID)
	if !meme.ShareExpiresAt.After(now) {
		meme.ShareGeneration++
		meme.SharedAt = now.UTC()
		meme.SharedByUserID = strings.TrimSpace(userID)
	}
	meme.ShareExpiresAt = expiresAt.UTC()
	if meme.ShareSlugs[meme.ShareGeneration] == "" {
		if meme.ShareSlugs == nil {
			meme.ShareSlugs = map[int64]string{}
		}
		meme.ShareSlugs[meme.ShareGeneration] = uuid.NewString()
	}
	d.writeIndex(t)
	return MemeShareState{
		MemeID:         meme.ID,
		Generation:     meme.ShareGeneration,
		SharedByUserID: meme.SharedByUserID,
		SharedAt:       meme.SharedAt,
		ExpiresAt:      meme.ShareExpiresAt,
		Slug:           meme.ShareSlugs[meme.ShareGeneration],
	}
}

func (d *legacyJSONDataDir) meme(t *testing.T, id string) *Meme {
	t.Helper()
	for i := range d.memes {
		if d.memes[i].ID == id {
			return &d.memes[i]
		}
	}
	t.Fatalf("legacy meme %s not found", id)
	return nil
}

// writeIndex writes index.json: a JSON array of persistedMeme, indented with
// two spaces.
func (d *legacyJSONDataDir) writeIndex(t *testing.T) {
	t.Helper()
	persisted := make([]persistedMeme, 0, len(d.memes))
	for _, meme := range d.memes {
		persisted = append(persisted, persistedMeme{
			ID:                  meme.ID,
			OriginalName:        meme.OriginalName,
			StoredName:          meme.StoredName,
			FilePath:            meme.FilePath,
			ContentType:         meme.ContentType,
			ContentHash:         meme.ContentHash,
			SizeBytes:           meme.SizeBytes,
			Tags:                meme.Tags,
			SuggestedTags:       meme.SuggestedTags,
			AutoSuggestDisabled: meme.AutoSuggestDisabled,
			Notes:               meme.Notes,
			SourceURL:           meme.SourceURL,
			CreatedAt:           meme.CreatedAt,
			UpdatedAt:           meme.UpdatedAt,
			ShareGeneration:     meme.ShareGeneration,
			ShareExpiresAt:      meme.ShareExpiresAt,
			SharedAt:            meme.SharedAt,
			SharedByUserID:      meme.SharedByUserID,
			ShareSlugs:          meme.ShareSlugs,
		})
	}
	d.writeJSON(t, "index.json", persisted)
}

// writeFavorites writes favorites.json: an object from user id (the local
// user is "__local__") to that user's sorted meme ids, omitting users with
// none.
func (d *legacyJSONDataDir) writeFavorites(t *testing.T) {
	t.Helper()
	raw := map[string][]string{}
	for userID, bucket := range d.favorites {
		memeIDs := make([]string, 0, len(bucket))
		for memeID := range bucket {
			memeIDs = append(memeIDs, memeID)
		}
		if len(memeIDs) == 0 {
			continue
		}
		sort.Strings(memeIDs)
		raw[userID] = memeIDs
	}
	d.writeJSON(t, "favorites.json", raw)
}

func (d *legacyJSONDataDir) writeJSON(t *testing.T, name string, value any) {
	t.Helper()
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("encode legacy %s: %v", name, err)
	}
	if err := os.WriteFile(filepath.Join(d.dir, name), payload, 0o644); err != nil {
		t.Fatalf("write legacy %s: %v", name, err)
	}
}
