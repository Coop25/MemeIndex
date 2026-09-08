package manager

import (
	"testing"

	"memeindex/internal/accessor"
)

type bulkFakeStore struct {
	*adminReadCacheStore
	bulkCalls int
	lastIDs   []string
}

func (s *bulkFakeStore) MemesByIDs(ids []string) (map[string]accessor.Meme, error) {
	s.bulkCalls++
	s.lastIDs = append([]string(nil), ids...)
	out := map[string]accessor.Meme{}
	for _, meme := range s.memes {
		for _, id := range ids {
			if meme.ID == id {
				out[id] = meme
			}
		}
	}
	return out, nil
}

var _ accessor.BulkMemeStore = (*bulkFakeStore)(nil)

func TestMemesByIDsUsesBatchWhenAvailable(t *testing.T) {
	store := &bulkFakeStore{adminReadCacheStore: &adminReadCacheStore{memes: []accessor.Meme{
		{ID: "a", OriginalName: "Alpha"},
		{ID: "b", OriginalName: "Bravo"},
	}}}
	m := NewMemeManager(store)

	got := m.memeNamesByIDs([]string{"a", "b", "missing"})

	if store.bulkCalls != 1 {
		t.Fatalf("bulk store queried %d times, want 1", store.bulkCalls)
	}
	if store.adminReadCacheStore.callCount() != 0 {
		t.Fatalf("per-id fallback ran %d times, want 0", store.adminReadCacheStore.callCount())
	}
	if got["a"] != "Alpha" || got["b"] != "Bravo" {
		t.Fatalf("names not resolved: %+v", got)
	}
	if _, ok := got["missing"]; ok {
		t.Fatalf("missing id should be absent, got %+v", got)
	}
}

func TestMemesByIDsFallsBackWithoutBulkCapability(t *testing.T) {
	store := &adminReadCacheStore{memes: []accessor.Meme{
		{ID: "a", OriginalName: "Alpha"},
		{ID: "b", OriginalName: "Bravo"},
	}}
	m := NewMemeManager(store)

	got := m.memesByIDs([]string{"a", "b"})

	// adminReadCacheStore has no BulkMemeStore, so the only way these names come
	// back is the per-id GetByID fallback path.
	if got["a"].OriginalName != "Alpha" || got["b"].OriginalName != "Bravo" {
		t.Fatalf("fallback did not resolve memes: %+v", got)
	}
}
