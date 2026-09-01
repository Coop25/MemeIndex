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
	Delete(input DeleteInput) (DeleteResult, error)
	UploadDir() string
}

type MemeShareState struct {
	MemeID         string    `json:"meme_id"`
	Generation     int64     `json:"generation"`
	SharedByUserID string    `json:"shared_by_user_id"`
	SharedAt       time.Time `json:"shared_at"`
	ExpiresAt      time.Time `json:"expires_at"`
}

type MemeShareStore interface {
	GetOrCreateMemeShare(memeID, userID string, now, expiresAt time.Time) (MemeShareState, error)
	GetMemeShareState(memeID string) (MemeShareState, error)
	ListActiveMemeShares(now time.Time) ([]MemeShareState, error)
	RevokeMemeShare(memeID string) error
	RevokeAllMemeShares(now time.Time) (int, error)
}

type AuditLogStore interface {
	ListMemeAudit(id string, limit int) ([]MemeAuditEntry, error)
	ListAuditFeed(offset int, limit int) (PagedAuditFeed, error)
	ListPendingDeletes(offset int, limit int) (PagedPendingDeletes, error)
	ApprovePendingDelete(id string, actor AuditActor) error
	RejectPendingDelete(id string, actor AuditActor) error
}

// AdminAnalyticsStore exposes aggregate-only values that are intentionally
// independent of the current viewer (for example, favorites across all users).
type AdminAnalyticsStore interface {
	TotalFavoriteAssignments() (int, error)
	FavoriteActivitySince(since time.Time) ([]AdminFavoriteActivity, error)
}

type AdminFavoriteActivity struct {
	Date    time.Time
	Added   int
	Removed int
}

// FavoriteAuditStore lets stores with an audit log record favorite changes in
// the same transaction as the favorite itself. Legacy/local stores can keep
// using Store.SetFavorite without implementing it.
type FavoriteAuditStore interface {
	SetFavoriteWithActor(userID, id string, favorite bool, actor AuditActor) (Meme, error)
}

type AdminMemeStore interface {
	GetAnyByID(id string) (Meme, error)
}

type PreviewAssetStore interface {
	ThumbnailDir() string
	EnsurePreviewAssets() error
}

type SuggestedTagStore interface {
	ListSuggestedTags(id string) ([]string, error)
	ReplaceSuggestedTags(id string, tags []string) error
	SetAutoSuggestDisabled(id string, disabled bool) error
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
