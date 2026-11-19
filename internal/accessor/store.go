package accessor

import (
	"context"
	"time"
)

type Store interface {
	List(userID, query string, favoritesOnly bool, tag string) []Meme
	SuggestTags(prefix string, limit int) []string
	GetByID(userID, id string) (Meme, error)
	Random(excludedIDs []string) (Meme, error)
	Create(input CreateInput) (Meme, error)
	// EditMeme applies a partial edit to one meme atomically against its
	// current state; see MemeEdit.
	EditMeme(userID, id string, edit MemeEdit) (Meme, error)
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

// DashboardRecentLimit is how many newest memes the home screen is given. The
// Recently added row shows as many of them as fit across the window, so this
// has to cover a very wide screen full of narrow portrait previews.
const DashboardRecentLimit = 24

// DashboardFavoriteLimit is how many newest favorites the home screen is
// given. Favorites fill a single fitted row just as Recently added does.
const DashboardFavoriteLimit = 24

// DashboardTopTagLimit is how many of the most used tags the home screen's
// tag cloud is given.
const DashboardTopTagLimit = 24

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

// The capability interfaces below group the store methods beyond Store by
// what they serve. PostgresStore, the only production store, implements every
// one of them (see the assertions at the end of this file). The manager still
// type-asserts each capability so test stores (accessortest.MemeStore and the
// narrow fakes in tests) can stand in for the parts a test exercises.

// QueryableMemeStore resolves browse, dashboard, and popular-tag reads in the
// database. They are the reads a viewer triggers most and that cost the most,
// so they take the request's context: a browser that navigates away cancels
// its query instead of leaving it to run to the statement timeout.
type QueryableMemeStore interface {
	QueryMemes(ctx context.Context, q MemeQuery) (MemeQueryPage, error)
	MemeDashboard(ctx context.Context, userID string) (MemeDashboardData, error)
	TagCounts(ctx context.Context, limit int) ([]TagCount, error)
}

type MemeShareState struct {
	MemeID         string    `json:"meme_id"`
	Generation     int64     `json:"generation"`
	SharedByUserID string    `json:"shared_by_user_id"`
	SharedAt       time.Time `json:"shared_at"`
	ExpiresAt      time.Time `json:"expires_at"`
	// Slug is the random code in the current share's public link. It is empty
	// for a share created before share links had codes.
	Slug string `json:"-"`
}

type MemeShareStore interface {
	// GetOrCreateMemeShare starts a share, or renews an active one: an active
	// share keeps its generation and link and only has its expiry moved to
	// expiresAt, while an expired or revoked one gets a new generation and a
	// new link.
	GetOrCreateMemeShare(memeID, userID string, now, expiresAt time.Time) (MemeShareState, error)
	GetMemeShareState(memeID string) (MemeShareState, error)
	// ResolveMemeShareSlug maps a share link code, current or historical, to
	// its meme and the share generation it was issued for. An unknown code
	// returns os.ErrNotExist.
	ResolveMemeShareSlug(slug string) (memeID string, generation int64, err error)
	ListActiveMemeShares(now time.Time) ([]MemeShareState, error)
	RevokeMemeShare(memeID string) error
	RevokeAllMemeShares(now time.Time) (int, error)
}

// AuditStore is the audit log: per-meme history, the admin feed, the
// pending-delete approval queue, entries not tied to one meme, and favorite
// changes recorded in the same transaction as the favorite itself.
type AuditStore interface {
	ListMemeAudit(id string, limit int) ([]MemeAuditEntry, error)
	ListAuditFeed(offset int, limit int, actorUserID string) (PagedAuditFeed, error)
	ListPendingDeletes(offset int, limit int) (PagedPendingDeletes, error)
	ApprovePendingDelete(id string, actor AuditActor) error
	RejectPendingDelete(id string, actor AuditActor) error
	// RecordSystemAudit records an entry that is not tied to a single meme
	// (for example, an admin resetting the tag-suggestion queue).
	RecordSystemAudit(action string, actor AuditActor, description string) error
	// CountAuditActionsByActor counts audit events of one action per actor.
	CountAuditActionsByActor(action string) (map[string]int, error)
	SetFavoriteWithActor(userID, id string, favorite bool, actor AuditActor) (Meme, error)
}

// FavoriteAnalyticsStore exposes favorite aggregates that are intentionally
// independent of the current viewer.
type FavoriteAnalyticsStore interface {
	// TotalFavoriteAssignments counts favorites across all users.
	TotalFavoriteAssignments() (int, error)
	FavoriteActivitySince(since time.Time) ([]AdminFavoriteActivity, error)
	// CountFavoritesByUser counts favorites per user.
	CountFavoritesByUser() (map[string]int, error)
}

type AdminFavoriteActivity struct {
	Date    time.Time
	Added   int
	Removed int
}

// AdminMemeStore serves the admin views, which need memes regardless of who is
// looking and, for storage and approval work, regardless of visibility.
type AdminMemeStore interface {
	// GetAnyByID returns a meme without applying a viewer's favorites.
	GetAnyByID(id string) (Meme, error)
	// MemesByIDs resolves many visible memes by id in a single query. It backs
	// the admin panels that would otherwise issue one GetByID per queued or
	// shared meme on every poll.
	MemesByIDs(ids []string) (map[string]Meme, error)
	// AdminDashboardMemes returns every visible meme, newest first, without the
	// per-row favourites subquery List would add: the admin overview takes
	// favourite totals from FavoriteAnalyticsStore instead.
	AdminDashboardMemes() ([]Meme, error)
	// AllStoredFiles lists every meme row regardless of visibility. The admin
	// storage scan uses it to decide which files under uploads/ and
	// thumbnails/ are referenced: the visible-only listings would make a
	// hidden or pending-delete meme's files look orphaned.
	AllStoredFiles() ([]StoredFile, error)
	// HiddenMemeIDs lists memes hidden from the app without being deleted (a
	// delete request awaiting approval), so their files stop being served.
	HiddenMemeIDs() ([]string, error)
}

// TagAdminStore resolves tag housekeeping and the tag-suggestion admin panel in
// the database instead of scanning or rewriting every meme.
type TagAdminStore interface {
	// MergeTag repoints every meme_tags link from sourceTag onto targetTag,
	// creating targetTag if needed and deleting sourceTag, in one transaction.
	// Audiobook tag links and account audiobook-tag
	// restrictions are re-pointed too. It returns how many memes, audiobooks
	// and restrictions carried sourceTag.
	MergeTag(sourceTag, targetTag string, actor AuditActor) (TagMergeCounts, error)
	// UntaggedWithoutSuggestionsCount counts visible memes with no tags, no
	// stored suggestions, and auto-suggest still enabled.
	UntaggedWithoutSuggestionsCount() (int, error)
	// UntaggedWithoutSuggestionIDs returns the ids of those same memes so the
	// suggestion queue can be seeded without loading every meme.
	UntaggedWithoutSuggestionIDs() ([]string, error)
	// PendingSuggestionMemes returns the total number of visible memes that have
	// stored suggestions plus one newest-first page of them.
	PendingSuggestionMemes(offset, limit int) (total int, memes []Meme, err error)
	// TagUsageCounts returns how many visible memes carry each tag.
	TagUsageCounts() (map[string]int, error)
}

// SuggestedTagStore keeps the tag-suggestion pass's output: the suggested tags
// awaiting review, the per-meme opt-out, and the free-text blob (on-image text
// plus audio transcript) that is folded into search.
type SuggestedTagStore interface {
	ListSuggestedTags(id string) ([]string, error)
	ReplaceSuggestedTags(id string, tags []string) error
	SetAutoSuggestDisabled(id string, disabled bool) error
	SetSearchText(id, text string) error
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

// StoredFile names the on-disk files of one meme row, hidden and
// pending-delete rows included.
type StoredFile struct {
	ID         string
	Title      string
	StoredName string
	FilePath   string
	Hidden     bool
	SizeBytes  int64
}

// PostgresStore implements every capability.
var (
	_ Store                  = (*PostgresStore)(nil)
	_ QueryableMemeStore     = (*PostgresStore)(nil)
	_ MemeShareStore         = (*PostgresStore)(nil)
	_ AuditStore             = (*PostgresStore)(nil)
	_ FavoriteAnalyticsStore = (*PostgresStore)(nil)
	_ AdminMemeStore         = (*PostgresStore)(nil)
	_ TagAdminStore          = (*PostgresStore)(nil)
	_ SuggestedTagStore      = (*PostgresStore)(nil)
	_ PreviewAssetStore      = (*PostgresStore)(nil)
	_ ReelSessionPersistence = (*PostgresStore)(nil)
)
