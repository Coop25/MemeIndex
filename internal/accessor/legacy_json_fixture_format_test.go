package accessor

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// legacyFormatSampleMemes covers every field the JSON store persisted: a
// shared meme with link codes for two generations, suggested tags and a source
// link, and a never-shared one with auto-suggest turned off and zero share
// times.
func legacyFormatSampleMemes() []Meme {
	return []Meme{
		{
			ID:              "1f0c0a9e-0000-6000-8000-000000000001",
			OriginalName:    "cat.png",
			StoredName:      "1f0c0a9e-0000-6000-8000-000000000001.png",
			FilePath:        "/uploads/1f0c0a9e-0000-6000-8000-000000000001.png",
			ContentType:     "image/png",
			ContentHash:     "4b1f0a6c2d9e8f7a6b5c4d3e2f1a0b9c8d7e6f5a4b3c2d1e0f9a8b7c6d5e4f3a",
			SizeBytes:       16,
			Tags:            []string{"cat", "reaction"},
			SuggestedTags:   []string{"funny", "orange"},
			Notes:           "the classic",
			SourceURL:       "https://example.com/cat",
			CreatedAt:       time.Date(2025, 3, 1, 10, 0, 0, 0, time.UTC),
			UpdatedAt:       time.Date(2025, 3, 2, 11, 30, 0, 123456789, time.UTC),
			ShareGeneration: 2,
			ShareExpiresAt:  time.Date(2026, 3, 5, 9, 15, 0, 0, time.UTC),
			SharedAt:        time.Date(2025, 3, 5, 9, 15, 0, 0, time.UTC),
			SharedByUserID:  "discord-user-1",
			ShareSlugs:      map[int64]string{1: "0b8d1c34-5f6e-4a7b-8c9d-0e1f2a3b4c5d", 2: "6e7f8a9b-0c1d-4e2f-8a3b-4c5d6e7f8a9b"},
		},
		{
			ID:                  "1f0c0a9e-0000-6000-8000-000000000002",
			OriginalName:        "dog.mp4",
			StoredName:          "1f0c0a9e-0000-6000-8000-000000000002.mp4",
			FilePath:            "/uploads/1f0c0a9e-0000-6000-8000-000000000002.mp4",
			ContentType:         "video/mp4",
			ContentHash:         "9a8b7c6d5e4f3a2b1c0d9e8f7a6b5c4d3e2f1a0b9c8d7e6f5a4b3c2d1e0f9a8b",
			SizeBytes:           16,
			Tags:                []string{},
			AutoSuggestDisabled: true,
			CreatedAt:           time.Date(2025, 4, 1, 8, 0, 0, 0, time.UTC),
			UpdatedAt:           time.Date(2025, 4, 1, 8, 5, 0, 0, time.UTC),
		},
	}
}

func legacyFormatSampleFavorites() map[string]map[string]struct{} {
	return map[string]map[string]struct{}{
		"discord-user-1": {"1f0c0a9e-0000-6000-8000-000000000001": {}},
		"__local__":      {"1f0c0a9e-0000-6000-8000-000000000002": {}, "1f0c0a9e-0000-6000-8000-000000000001": {}},
		"discord-user-2": {"1f0c0a9e-0000-6000-8000-000000000002": {}},
	}
}

// The upgrade tests write their JSON-store data directories with
// legacyJSONDataDir. These golden files were produced by the JSON MemeStore's
// own save code from the sample above before that store was removed, so the
// fixture keeps writing exactly what PostgresStore's import has to read.
func TestLegacyJSONDataDirMatchesJSONStoreFormat(t *testing.T) {
	d := newLegacyJSONDataDir(t, t.TempDir())
	d.memes = legacyFormatSampleMemes()
	d.favorites = legacyFormatSampleFavorites()
	d.writeIndex(t)
	d.writeFavorites(t)

	for name, want := range map[string]string{
		"index.json":     legacyIndexGolden,
		"favorites.json": legacyFavoritesGolden,
	} {
		got, err := os.ReadFile(filepath.Join(d.dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if string(got) != want {
			t.Errorf("%s differs from the JSON store's format:\ngot:\n%s\nwant:\n%s", name, got, want)
		}
	}
}

const legacyIndexGolden = `[
  {
    "id": "1f0c0a9e-0000-6000-8000-000000000001",
    "originalName": "cat.png",
    "storedName": "1f0c0a9e-0000-6000-8000-000000000001.png",
    "filePath": "/uploads/1f0c0a9e-0000-6000-8000-000000000001.png",
    "contentType": "image/png",
    "contentHash": "4b1f0a6c2d9e8f7a6b5c4d3e2f1a0b9c8d7e6f5a4b3c2d1e0f9a8b7c6d5e4f3a",
    "sizeBytes": 16,
    "tags": [
      "cat",
      "reaction"
    ],
    "suggestedTags": [
      "funny",
      "orange"
    ],
    "notes": "the classic",
    "sourceUrl": "https://example.com/cat",
    "createdAt": "2025-03-01T10:00:00Z",
    "updatedAt": "2025-03-02T11:30:00.123456789Z",
    "shareGeneration": 2,
    "shareExpiresAt": "2026-03-05T09:15:00Z",
    "sharedAt": "2025-03-05T09:15:00Z",
    "sharedByUserId": "discord-user-1",
    "shareSlugs": {
      "1": "0b8d1c34-5f6e-4a7b-8c9d-0e1f2a3b4c5d",
      "2": "6e7f8a9b-0c1d-4e2f-8a3b-4c5d6e7f8a9b"
    }
  },
  {
    "id": "1f0c0a9e-0000-6000-8000-000000000002",
    "originalName": "dog.mp4",
    "storedName": "1f0c0a9e-0000-6000-8000-000000000002.mp4",
    "filePath": "/uploads/1f0c0a9e-0000-6000-8000-000000000002.mp4",
    "contentType": "video/mp4",
    "contentHash": "9a8b7c6d5e4f3a2b1c0d9e8f7a6b5c4d3e2f1a0b9c8d7e6f5a4b3c2d1e0f9a8b",
    "sizeBytes": 16,
    "tags": [],
    "autoSuggestDisabled": true,
    "notes": "",
    "createdAt": "2025-04-01T08:00:00Z",
    "updatedAt": "2025-04-01T08:05:00Z",
    "shareExpiresAt": "0001-01-01T00:00:00Z",
    "sharedAt": "0001-01-01T00:00:00Z"
  }
]`

const legacyFavoritesGolden = `{
  "__local__": [
    "1f0c0a9e-0000-6000-8000-000000000001",
    "1f0c0a9e-0000-6000-8000-000000000002"
  ],
  "discord-user-1": [
    "1f0c0a9e-0000-6000-8000-000000000001"
  ],
  "discord-user-2": [
    "1f0c0a9e-0000-6000-8000-000000000002"
  ]
}`
