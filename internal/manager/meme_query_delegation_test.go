package manager

import (
	"errors"
	"testing"
	"time"

	"memeindex/internal/accessor"
)

// queryableFakeStore embeds the in-memory fake and adds the optional
// QueryableMemeStore capability so the manager's SQL-delegation path is exercised.
type queryableFakeStore struct {
	*adminReadCacheStore

	queryCalls int
	lastQuery  accessor.MemeQuery
	page       accessor.MemeQueryPage
	dashboard  accessor.MemeDashboardData
	tagCounts  []accessor.TagCount
	err        error
}

func (s *queryableFakeStore) QueryMemes(q accessor.MemeQuery) (accessor.MemeQueryPage, error) {
	s.queryCalls++
	s.lastQuery = q
	if s.err != nil {
		return accessor.MemeQueryPage{}, s.err
	}
	return s.page, nil
}

func (s *queryableFakeStore) MemeDashboard(string) (accessor.MemeDashboardData, error) {
	if s.err != nil {
		return accessor.MemeDashboardData{}, s.err
	}
	return s.dashboard, nil
}

func (s *queryableFakeStore) TagCounts(int) ([]accessor.TagCount, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.tagCounts, nil
}

var _ accessor.QueryableMemeStore = (*queryableFakeStore)(nil)

func newQueryableFakeStore(memes ...accessor.Meme) *queryableFakeStore {
	return &queryableFakeStore{adminReadCacheStore: &adminReadCacheStore{memes: memes}}
}

func TestListMemesSortedDelegatesToStore(t *testing.T) {
	store := newQueryableFakeStore()
	store.page = accessor.MemeQueryPage{
		Memes:      []accessor.Meme{{ID: "z"}},
		Counts:     accessor.MemeCategoryCounts{Total: 9, Favorites: 3, Videos: 2, Images: 4, MP3s: 1, Untagged: 5, Files: 2},
		HasMore:    true,
		NextOffset: 99,
	}
	m := NewMemeManager(store)

	got := m.ListMemesSorted("user-1", "  cat  ", false, "", "videos", "oldest", 0, 50)

	if store.queryCalls != 1 {
		t.Fatalf("QueryMemes called %d times, want 1", store.queryCalls)
	}
	if store.adminReadCacheStore.callCount() != 0 {
		t.Fatalf("in-memory List was used %d times, want 0", store.adminReadCacheStore.callCount())
	}
	if store.lastQuery.Search != "cat" || store.lastQuery.View != "videos" || store.lastQuery.Sort != "oldest" || store.lastQuery.Limit != 50 {
		t.Fatalf("query passed through incorrectly: %+v", store.lastQuery)
	}
	if len(got.Memes) != 1 || got.Memes[0].ID != "z" {
		t.Fatalf("Memes = %+v", got.Memes)
	}
	if got.Counts.Videos != 2 || got.Counts.Untagged != 5 || got.Counts.Files != 2 || got.Counts.Total != 9 {
		t.Fatalf("Counts not mapped: %+v", got.Counts)
	}
	if !got.HasMore || got.NextOffset != 99 {
		t.Fatalf("pagination not mapped: HasMore=%v NextOffset=%d", got.HasMore, got.NextOffset)
	}
}

func TestListMemesSortedClampsLimit(t *testing.T) {
	store := newQueryableFakeStore()
	m := NewMemeManager(store)

	m.ListMemesSorted("u", "", false, "", "", "", -5, 100000)

	if store.lastQuery.Limit != maxMemePageLimit {
		t.Fatalf("Limit = %d, want clamp to %d", store.lastQuery.Limit, maxMemePageLimit)
	}
	if store.lastQuery.Offset != 0 {
		t.Fatalf("Offset = %d, want 0 for a negative input", store.lastQuery.Offset)
	}
}

func TestListMemesSortedFallsBackWhenStoreErrors(t *testing.T) {
	now := time.Now().UTC()
	store := newQueryableFakeStore(
		accessor.Meme{ID: "a", ContentType: "image/png", CreatedAt: now},
		accessor.Meme{ID: "b", ContentType: "video/mp4", CreatedAt: now.Add(-time.Hour)},
	)
	store.err = errors.New("db unavailable")
	m := NewMemeManager(store)

	got := m.ListMemesSorted("u", "", false, "", "", "newest", 0, 10)

	if store.adminReadCacheStore.callCount() == 0 {
		t.Fatal("expected fallback to the in-memory List after the query error")
	}
	if got.Counts.Total != 2 || len(got.Memes) != 2 {
		t.Fatalf("fallback result wrong: counts=%+v memes=%d", got.Counts, len(got.Memes))
	}
}

func TestDashboardDelegatesToStore(t *testing.T) {
	store := newQueryableFakeStore()
	store.dashboard = accessor.MemeDashboardData{
		TotalItems:   7,
		Favorites:    2,
		StorageBytes: 4096,
		TagCount:     5,
		Counts:       accessor.MemeCategoryCounts{Total: 7, Images: 6, Files: 1},
		RecentItems:  []accessor.Meme{{ID: "r1"}},
		TopTags:      []accessor.TagCount{{Name: "cat", Count: 4}, {Name: "dog", Count: 1}},
	}
	m := NewMemeManager(store)

	got := m.Dashboard("u")

	if store.adminReadCacheStore.callCount() != 0 {
		t.Fatalf("in-memory List used %d times, want 0", store.adminReadCacheStore.callCount())
	}
	if got.TotalItems != 7 || got.StorageBytes != 4096 || got.TagCount != 5 || got.Counts.Images != 6 {
		t.Fatalf("dashboard not mapped: %+v", got)
	}
	if len(got.RecentItems) != 1 || got.RecentItems[0].ID != "r1" {
		t.Fatalf("RecentItems = %+v", got.RecentItems)
	}
	if got.FavoriteItems == nil || got.RandomItems == nil {
		t.Fatalf("nil item slices must be normalised to empty for JSON")
	}
	if len(got.TopTags) != 2 || got.TopTags[0].Name != "cat" || got.TopTags[0].Count != 4 {
		t.Fatalf("TopTags = %+v", got.TopTags)
	}
}

func TestPopularTagsDelegatesToStore(t *testing.T) {
	store := newQueryableFakeStore()
	store.tagCounts = []accessor.TagCount{{Name: "reaction", Count: 12}, {Name: "cat", Count: 9}}
	m := NewMemeManager(store)

	got := m.PopularTags("u", 10)

	if store.adminReadCacheStore.callCount() != 0 {
		t.Fatalf("in-memory List used %d times, want 0", store.adminReadCacheStore.callCount())
	}
	if len(got) != 2 || got[0].Name != "reaction" || got[0].Count != 12 || got[1].Name != "cat" {
		t.Fatalf("PopularTags = %+v", got)
	}
}
