package manager

import (
	"io"
	"net/textproto"
	"path/filepath"
	"strings"

	"memeindex/internal/accessor"
)

type MemeManager struct {
	store        accessor.Store
	reelSessions *ReelSessionStore
}

type MemeCounts struct {
	Total     int `json:"total"`
	Favorites int `json:"favorites"`
	Videos    int `json:"videos"`
	Images    int `json:"images"`
	MP3s      int `json:"mp3s"`
	Untagged  int `json:"untagged"`
	Files     int `json:"files"`
}

type MemeListResult struct {
	Memes      []accessor.Meme `json:"memes"`
	Counts     MemeCounts      `json:"counts"`
	HasMore    bool            `json:"has_more"`
	NextOffset int             `json:"next_offset"`
}

func NewMemeManager(store accessor.Store) *MemeManager {
	sessionFile := filepath.Join(filepath.Dir(store.UploadDir()), "reel_sessions.json")
	return &MemeManager{
		store:        store,
		reelSessions: NewReelSessionStore(store, sessionFile),
	}
}

func (m *MemeManager) ListMemes(userID, query string, favoritesOnly bool, tag string, view string, offset int, limit int) MemeListResult {
	source := m.store.List(strings.TrimSpace(userID), strings.TrimSpace(query), false, strings.TrimSpace(tag))
	counts := buildMemeCounts(source)
	visible := filterMemesByView(source, strings.TrimSpace(view))
	if favoritesOnly {
		visible = filterMemesByView(visible, "favorites")
	}

	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 72
	}

	start := min(offset, len(visible))
	end := min(offset+limit, len(visible))
	page := visible[start:end]

	return MemeListResult{
		Memes:      append([]accessor.Meme(nil), page...),
		Counts:     counts,
		HasMore:    end < len(visible),
		NextOffset: end,
	}
}

func (m *MemeManager) CreateMeme(file io.Reader, header textproto.MIMEHeader, filename string, tags []string, notes string) (accessor.Meme, error) {
	return m.store.Create(accessor.CreateInput{
		File:     file,
		Header:   header,
		Filename: filename,
		Tags:     normalizeTags(tags),
		Notes:    strings.TrimSpace(notes),
	})
}

func (m *MemeManager) UpdateMeme(userID, id string, update accessor.MemeUpdate) (accessor.Meme, error) {
	update.Tags = normalizeTags(update.Tags)
	update.Notes = strings.TrimSpace(update.Notes)
	return m.store.Update(strings.TrimSpace(userID), strings.TrimSpace(id), update)
}

func (m *MemeManager) SetFavorite(userID, id string, favorite bool) (accessor.Meme, error) {
	return m.store.SetFavorite(strings.TrimSpace(userID), strings.TrimSpace(id), favorite)
}

func (m *MemeManager) DeleteMeme(id string) error {
	return m.store.Delete(strings.TrimSpace(id))
}

func (m *MemeManager) SuggestTags(prefix string, limit int) []string {
	return m.store.SuggestTags(strings.TrimSpace(prefix), limit)
}

func (m *MemeManager) RandomMeme(excludedIDs []string) (accessor.Meme, error) {
	normalized := make([]string, 0, len(excludedIDs))
	for _, id := range excludedIDs {
		trimmed := strings.TrimSpace(id)
		if trimmed == "" {
			continue
		}
		normalized = append(normalized, trimmed)
	}
	return m.store.Random(normalized)
}

func (m *MemeManager) StepRandomReel(sessionID string, direction string) (ReelStepResult, error) {
	return m.reelSessions.Step(sessionID, direction)
}

func (m *MemeManager) DeleteRandomReelSession(sessionID string) error {
	return m.reelSessions.Delete(sessionID)
}

func (m *MemeManager) CleanupStaleReelSessions() error {
	return m.reelSessions.CleanupStale()
}

func (m *MemeManager) UploadDir() string {
	return m.store.UploadDir()
}

func normalizeTags(tags []string) []string {
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		trimmed := strings.ToLower(strings.TrimSpace(tag))
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

func buildMemeCounts(memes []accessor.Meme) MemeCounts {
	counts := MemeCounts{}
	for _, meme := range memes {
		counts.Total += 1
		if meme.Favorite {
			counts.Favorites += 1
		}
		switch {
		case strings.HasPrefix(meme.ContentType, "video/"):
			counts.Videos += 1
		case strings.HasPrefix(meme.ContentType, "image/"):
			counts.Images += 1
		case meme.ContentType == "audio/mpeg" || strings.HasSuffix(strings.ToLower(meme.OriginalName), ".mp3"):
			counts.MP3s += 1
		default:
			counts.Files += 1
		}
		if len(meme.Tags) == 0 {
			counts.Untagged += 1
		}
	}
	return counts
}

func filterMemesByView(memes []accessor.Meme, view string) []accessor.Meme {
	switch view {
	case "favorites":
		return filterMemes(memes, func(meme accessor.Meme) bool { return meme.Favorite })
	case "videos":
		return filterMemes(memes, func(meme accessor.Meme) bool { return strings.HasPrefix(meme.ContentType, "video/") })
	case "images":
		return filterMemes(memes, func(meme accessor.Meme) bool { return strings.HasPrefix(meme.ContentType, "image/") })
	case "mp3s":
		return filterMemes(memes, func(meme accessor.Meme) bool {
			return meme.ContentType == "audio/mpeg" || strings.HasSuffix(strings.ToLower(meme.OriginalName), ".mp3")
		})
	case "untagged":
		return filterMemes(memes, func(meme accessor.Meme) bool { return len(meme.Tags) == 0 })
	case "files":
		return filterMemes(memes, func(meme accessor.Meme) bool {
			return !strings.HasPrefix(meme.ContentType, "image/") &&
				!strings.HasPrefix(meme.ContentType, "video/") &&
				meme.ContentType != "audio/mpeg" &&
				!strings.HasSuffix(strings.ToLower(meme.OriginalName), ".mp3")
		})
	default:
		return memes
	}
}

func filterMemes(memes []accessor.Meme, keep func(accessor.Meme) bool) []accessor.Meme {
	filtered := make([]accessor.Meme, 0, len(memes))
	for _, meme := range memes {
		if keep(meme) {
			filtered = append(filtered, meme)
		}
	}
	return filtered
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
