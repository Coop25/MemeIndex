package accessor

import "time"

type Store interface {
	List(userID, query string, favoritesOnly bool, tag string) []Meme
	SuggestTags(prefix string, limit int) []string
	GetByID(userID, id string) (Meme, error)
	Random(excludedIDs []string) (Meme, error)
	Create(input CreateInput) (Meme, error)
	Update(userID, id string, update MemeUpdate) (Meme, error)
	SetFavorite(userID, id string, favorite bool) (Meme, error)
	Delete(id string) error
	UploadDir() string
}

type PreviewAssetStore interface {
	ThumbnailDir() string
	EnsurePreviewAssets() error
}

type ReelSessionRecord struct {
	History      []string
	Position     int
	LastActivity time.Time
}

type ReelSessionPersistence interface {
	LoadReelSessions() (map[string]ReelSessionRecord, error)
	SaveReelSession(sessionID string, session ReelSessionRecord) error
	DeleteReelSession(sessionID string) error
	CleanupStaleReelSessions(before time.Time) error
}
