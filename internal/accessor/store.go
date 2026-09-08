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

// MemeQuery describes a single page of a browse/search request. It is resolved
// entirely in the database by stores that implement QueryableMemeStore, instead
// of loading the whole archive into the application and filtering in memory.
type MemeQuery struct {
	UserID        string
	Search        string
	Tag           string
	View          string
	FavoritesOnly bool
	Sort          string
	Offset        int
	Limit         int
}

// MemeCategoryCounts mirrors the facet counts the UI shows above the grid. The
// seven category fields (Videos/Images/MP3s/Files) are mutually exclusive and
// sum to Total; Favorites and Untagged are independent overlays. Counts reflect
// the search and tag filter only, not the view or favorites-only filter, which
// matches the pre-existing in-memory behaviour.
type MemeCategoryCounts struct {
	Total     int
	Favorites int
	Videos    int
	Images    int
	MP3s      int
	Untagged  int
	Files     int
}

// MemeQueryPage is one resolved page of MemeQuery.
type MemeQueryPage struct {
	Memes      []Meme
	Counts     MemeCategoryCounts
	HasMore    bool
	NextOffset int
}

// TagCount is a tag name paired with how many visible memes carry it.
type TagCount struct {
	Name  string
	Count int
}

// MemeDashboardData is the SQL-resolved form of the user home screen summary.
type MemeDashboardData struct {
	TotalItems    int
	Favorites     int
	StorageBytes  int64
	TagCount      int
	Counts        MemeCategoryCounts
	RecentItems   []Meme
	FavoriteItems []Meme
	RandomItems   []Meme
	TopTags       []TagCount
}

// QueryableMemeStore is an optional capability: a store that can resolve browse,
// dashboard, and popular-tag reads in the database. The manager falls back to
// the in-memory Store methods for stores that do not implement it.
type QueryableMemeStore interface {
	QueryMemes(q MemeQuery) (MemeQueryPage, error)
	MemeDashboard(userID string) (MemeDashboardData, error)
	TagCounts(limit int) ([]TagCount, error)
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

// SystemAuditStore records audit entries that are not tied to a single meme
// (for example, an admin resetting the tag-suggestion queue). Stores without an
// audit log simply do not implement it.
type SystemAuditStore interface {
	RecordSystemAudit(action string, actor AuditActor, description string) error
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

// SearchTextStore is an optional capability: a store that keeps a per-meme
// free-text blob (on-image text + audio transcript, produced by the
// tag-suggestion pass) and folds it into search. Stores without it simply do
// not index that text.
type SearchTextStore interface {
	SetSearchText(id, text string) error
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
