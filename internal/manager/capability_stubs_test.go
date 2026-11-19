package manager

import (
	"context"
	"slices"

	"memeindex/internal/accessor"
)

// The fakes in this package are built up one capability group at a time. The
// methods here fill the rest of a group with plain, data-backed behaviour so
// a fake that overrides one method of a group still satisfies the whole
// interface.

// adminReadCacheStore records system audits; the rest of AuditStore is empty.
func (s *adminReadCacheStore) ListMemeAudit(context.Context, string, int) ([]accessor.MemeAuditEntry, error) {
	return nil, nil
}
func (s *adminReadCacheStore) ListAuditFeed(context.Context, int, int, string) (accessor.PagedAuditFeed, error) {
	return accessor.PagedAuditFeed{}, nil
}
func (s *adminReadCacheStore) ListPendingDeletes(context.Context, int, int) (accessor.PagedPendingDeletes, error) {
	return accessor.PagedPendingDeletes{}, nil
}
func (s *adminReadCacheStore) ApprovePendingDelete(context.Context, string, accessor.AuditActor) error {
	return nil
}
func (s *adminReadCacheStore) RejectPendingDelete(context.Context, string, accessor.AuditActor) error {
	return nil
}
func (s *adminReadCacheStore) CountAuditActionsByActor(context.Context, string) (map[string]int, error) {
	return map[string]int{}, nil
}
func (s *adminReadCacheStore) SetFavoriteWithActor(ctx context.Context, userID, id string, favorite bool, _ accessor.AuditActor) (accessor.Meme, error) {
	return s.SetFavorite(ctx, userID, id, favorite)
}
func (s *adminReadCacheStore) SetSearchText(context.Context, string, string) error { return nil }

var _ accessor.AuditStore = (*adminReadCacheStore)(nil)

// adminMemeReadCache adds AdminMemeStore over adminReadCacheStore's memes. A
// fake embeds it and overrides the one method its test watches.
type adminMemeReadCache struct {
	*adminReadCacheStore
}

func (s adminMemeReadCache) GetAnyByID(ctx context.Context, id string) (accessor.Meme, error) {
	return s.GetByID(ctx, "", id)
}

func (s adminMemeReadCache) MemesByIDs(ctx context.Context, ids []string) (map[string]accessor.Meme, error) {
	out := map[string]accessor.Meme{}
	for _, id := range ids {
		if meme, err := s.GetByID(ctx, "", id); err == nil {
			out[id] = meme
		}
	}
	return out, nil
}

func (s adminMemeReadCache) AdminDashboardMemes(_ context.Context) ([]accessor.Meme, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]accessor.Meme(nil), s.memes...), nil
}

func (s adminMemeReadCache) AllStoredFiles(_ context.Context) ([]accessor.StoredFile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]accessor.StoredFile, 0, len(s.memes))
	for _, meme := range s.memes {
		out = append(out, accessor.StoredFile{ID: meme.ID, Title: meme.OriginalName, StoredName: meme.StoredName, FilePath: meme.FilePath, SizeBytes: meme.SizeBytes})
	}
	return out, nil
}

func (s adminMemeReadCache) HiddenMemeIDs(_ context.Context) ([]string, error) { return nil, nil }

func (s *dashboardMetricStore) CountFavoritesByUser(_ context.Context) (map[string]int, error) {
	return map[string]int{}, nil
}

func (s *tagReviewTestStore) SetSearchText(context.Context, string, string) error { return nil }

func (s *tagReviewTestStore) RemoveSuggestedTag(ctx context.Context, id, tag string) error {
	tags, err := s.ListSuggestedTags(ctx, id)
	if err != nil {
		return err
	}
	return s.ReplaceSuggestedTags(ctx, id, slices.DeleteFunc(tags, func(t string) bool { return t == tag }))
}

func (s *adminReadCacheStore) RemoveSuggestedTag(ctx context.Context, id, tag string) error {
	tags, err := s.ListSuggestedTags(ctx, id)
	if err != nil {
		return err
	}
	return s.ReplaceSuggestedTags(ctx, id, slices.DeleteFunc(tags, func(t string) bool { return t == tag }))
}
