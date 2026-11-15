package accessor

import (
	"os"
	"slices"
	"strings"
)

// The methods below complete MemeStore's AdminMemeStore and SuggestedTagStore
// capabilities so tests built on it exercise the same manager paths as
// PostgresStore. MemeStore has no visibility states, so nothing is ever
// hidden, and it keeps no search index, so search text is accepted and
// dropped.

var (
	_ AdminMemeStore         = (*MemeStore)(nil)
	_ SuggestedTagStore      = (*MemeStore)(nil)
	_ FavoriteAnalyticsStore = (*MemeStore)(nil)
	_ MemeShareStore         = (*MemeStore)(nil)
	_ PreviewAssetStore      = (*MemeStore)(nil)
)

// GetAnyByID returns a meme without a viewer's favorite flag.
func (s *MemeStore) GetAnyByID(id string) (Meme, error) {
	return s.GetByID("", id)
}

// MemesByIDs returns the memes among ids that exist, keyed by id.
func (s *MemeStore) MemesByIDs(ids []string) (map[string]Meme, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]Meme, len(ids))
	for _, id := range ids {
		if meme, ok := s.byID[strings.TrimSpace(id)]; ok {
			decoratePreviewPath(&meme, s.previewDir)
			out[meme.ID] = meme
		}
	}
	return out, nil
}

// AdminDashboardMemes returns every meme, newest first.
func (s *MemeStore) AdminDashboardMemes() ([]Meme, error) {
	s.mu.RLock()
	out := slices.Clone(s.memes)
	s.mu.RUnlock()
	for i := range out {
		decoratePreviewPath(&out[i], s.previewDir)
	}
	slices.SortStableFunc(out, func(a, b Meme) int { return b.CreatedAt.Compare(a.CreatedAt) })
	return out, nil
}

// HiddenMemeIDs is always empty: MemeStore cannot hide a meme.
func (s *MemeStore) HiddenMemeIDs() ([]string, error) {
	return nil, nil
}

// SetSearchText checks the meme exists and otherwise does nothing.
func (s *MemeStore) SetSearchText(id, _ string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.byID[strings.TrimSpace(id)]; !ok {
		return os.ErrNotExist
	}
	return nil
}
