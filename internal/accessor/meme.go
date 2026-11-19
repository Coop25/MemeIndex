package accessor

import (
	"io"
	"mime"
	"net/textproto"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Meme struct {
	ID                  string    `json:"id"`
	OriginalName        string    `json:"originalName"`
	StoredName          string    `json:"storedName"`
	FilePath            string    `json:"filePath"`
	PreviewPath         string    `json:"previewPath,omitempty"`
	ContentType         string    `json:"contentType"`
	ContentHash         string    `json:"-"`
	SizeBytes           int64     `json:"sizeBytes"`
	Tags                []string  `json:"tags"`
	SuggestedTags       []string  `json:"suggestedTags,omitempty"`
	AutoSuggestDisabled bool      `json:"autoSuggestDisabled,omitempty"`
	Notes               string    `json:"notes"`
	SourceURL           string    `json:"sourceUrl,omitempty"`
	Favorite            bool      `json:"favorite"`
	CreatedAt           time.Time `json:"createdAt"`
	UpdatedAt           time.Time `json:"updatedAt"`
	ShareGeneration     int64     `json:"-"`
	ShareExpiresAt      time.Time `json:"-"`
	SharedAt            time.Time `json:"-"`
	SharedByUserID      string    `json:"-"`
	// ShareSlugs maps each share generation to the link code issued for it.
	ShareSlugs map[int64]string `json:"-"`
}

type AuditActor struct {
	UserID       string `json:"user_id"`
	Username     string `json:"username"`
	DisplayName  string `json:"display_name"`
	AvatarURL    string `json:"avatar_url"`
	IsSuperAdmin bool   `json:"is_super_admin"`
}

type MemeAuditEntry struct {
	ID          int64      `json:"id"`
	MemeID      string     `json:"meme_id"`
	Action      string     `json:"action"`
	Actor       AuditActor `json:"actor"`
	Description string     `json:"description"`
	CreatedAt   time.Time  `json:"created_at"`
}

type GlobalMemeAuditEntry struct {
	MemeAuditEntry
	MemeOriginalName string `json:"meme_original_name"`
	MemeContentType  string `json:"meme_content_type"`
	MemeFilePath     string `json:"meme_file_path"`
}

type PagedAuditFeed struct {
	Events     []GlobalMemeAuditEntry `json:"events"`
	Total      int                    `json:"total"`
	HasMore    bool                   `json:"has_more"`
	NextOffset int                    `json:"next_offset"`
}

type PagedPendingDeletes struct {
	Memes      []PendingDeleteRecord `json:"memes"`
	Total      int                   `json:"total"`
	HasMore    bool                  `json:"has_more"`
	NextOffset int                   `json:"next_offset"`
}

type PendingDeleteRecord struct {
	Meme        Meme       `json:"meme"`
	RequestedBy AuditActor `json:"requested_by"`
	RequestedAt time.Time  `json:"requested_at"`
}

type DeleteInput struct {
	ID    string
	Actor AuditActor
}

type DeleteResult struct {
	Deleted         bool `json:"deleted"`
	PendingApproval bool `json:"pending_approval"`
}

type CreateInput struct {
	File        io.Reader
	Header      textproto.MIMEHeader
	Filename    string
	Tags        []string
	Notes       string
	SourceURL   string
	ContentType string
	Actor       AuditActor
}

// localFavoritesUserID is the favorites key for an empty user id. The JSON
// store used the same key in favorites.json, which the legacy import reads.
const localFavoritesUserID = "__local__"

func normalizeFavoriteUserID(userID string) string {
	trimmed := strings.TrimSpace(userID)
	if trimmed == "" {
		return localFavoritesUserID
	}
	return trimmed
}

func normalizeTags(tags []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		normalized := normalizeTag(tag)
		if normalized == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	sort.Strings(out)
	return out
}

func normalizeTag(tag string) string {
	return strings.ToLower(strings.TrimSpace(tag))
}

func detectContentType(header textproto.MIMEHeader, explicitContentType, filename string) string {
	if strings.TrimSpace(explicitContentType) != "" {
		return explicitContentType
	}

	if contentType := header.Get("Content-Type"); contentType != "" {
		return contentType
	}

	if ext := filepath.Ext(filename); ext != "" {
		if guessed := mime.TypeByExtension(ext); guessed != "" {
			return guessed
		}
	}

	return "application/octet-stream"
}
