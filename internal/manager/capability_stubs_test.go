package manager

import (
	"slices"

	"memeindex/internal/accessor"
)

// The fakes in this package are built up one capability group at a time. The
// methods here fill the rest of a group with plain, data-backed behaviour so
// a fake that overrides one method of a group still satisfies the whole
// interface.

// adminReadCacheStore records system audits; the rest of AuditStore is empty.
func (s *adminReadCacheStore) ListMemeAudit(string, int) ([]accessor.MemeAuditEntry, error) {
	return nil, nil
}
func (s *adminReadCacheStore) ListAuditFeed(int, int, string) (accessor.PagedAuditFeed, error) {
	return accessor.PagedAuditFeed{}, nil
}
func (s *adminReadCacheStore) ListPendingDeletes(int, int) (accessor.PagedPendingDeletes, error) {
	return accessor.PagedPendingDeletes{}, nil
}
func (s *adminReadCacheStore) ApprovePendingDelete(string, accessor.AuditActor) error { return nil }
func (s *adminReadCacheStore) RejectPendingDelete(string, accessor.AuditActor) error  { return nil }
func (s *adminReadCacheStore) CountAuditActionsByActor(string) (map[string]int, error) {
	return map[string]int{}, nil
}
func (s *adminReadCacheStore) SetFavoriteWithActor(userID, id string, favorite bool, _ accessor.AuditActor) (accessor.Meme, error) {
	return s.SetFavorite(userID, id, favorite)
}
func (s *adminReadCacheStore) SetSearchText(string, string) error { return nil }

var _ accessor.AuditStore = (*adminReadCacheStore)(nil)

// adminMemeReadCache adds AdminMemeStore over adminReadCacheStore's memes. A
// fake embeds it and overrides the one method its test watches.
type adminMemeReadCache struct {
	*adminReadCacheStore
}

func (s adminMemeReadCache) GetAnyByID(id string) (accessor.Meme, error) {
	return s.GetByID("", id)
}

func (s adminMemeReadCache) MemesByIDs(ids []string) (map[string]accessor.Meme, error) {
	out := map[string]accessor.Meme{}
	for _, id := range ids {
		if meme, err := s.GetByID("", id); err == nil {
			out[id] = meme
		}
	}
	return out, nil
}

func (s adminMemeReadCache) AdminDashboardMemes() ([]accessor.Meme, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]accessor.Meme(nil), s.memes...), nil
}

func (s adminMemeReadCache) AllStoredFiles() ([]accessor.StoredFile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]accessor.StoredFile, 0, len(s.memes))
	for _, meme := range s.memes {
		out = append(out, accessor.StoredFile{ID: meme.ID, Title: meme.OriginalName, StoredName: meme.StoredName, FilePath: meme.FilePath, SizeBytes: meme.SizeBytes})
	}
	return out, nil
}

func (s adminMemeReadCache) HiddenMemeIDs() ([]string, error) { return nil, nil }

func (s *dashboardMetricStore) CountFavoritesByUser() (map[string]int, error) {
	return map[string]int{}, nil
}

func (s *tagReviewTestStore) SetSearchText(string, string) error { return nil }

func (s *tagReviewTestStore) RemoveSuggestedTag(id, tag string) error {
	tags, err := s.ListSuggestedTags(id)
	if err != nil {
		return err
	}
	return s.ReplaceSuggestedTags(id, slices.DeleteFunc(tags, func(t string) bool { return t == tag }))
}

func (s *adminReadCacheStore) RemoveSuggestedTag(id, tag string) error {
	tags, err := s.ListSuggestedTags(id)
	if err != nil {
		return err
	}
	return s.ReplaceSuggestedTags(id, slices.DeleteFunc(tags, func(t string) bool { return t == tag }))
}
