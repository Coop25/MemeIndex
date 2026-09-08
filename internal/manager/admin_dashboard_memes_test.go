package manager

import (
	"errors"
	"testing"
	"time"

	"memeindex/internal/accessor"
)

// adminDashboardMemeFakeStore adds the optional AdminDashboardMemeStore
// capability on top of the in-memory fake so the lean-scan delegation path is
// exercised.
type adminDashboardMemeFakeStore struct {
	*adminReadCacheStore

	leanCalls int
	leanMemes []accessor.Meme
	leanErr   error
}

func (s *adminDashboardMemeFakeStore) AdminDashboardMemes() ([]accessor.Meme, error) {
	s.leanCalls++
	if s.leanErr != nil {
		return nil, s.leanErr
	}
	return append([]accessor.Meme(nil), s.leanMemes...), nil
}

var _ accessor.AdminDashboardMemeStore = (*adminDashboardMemeFakeStore)(nil)

func TestAdminDashboardUsesLeanMemeScan(t *testing.T) {
	now := time.Now().UTC()
	memes := []accessor.Meme{
		{ID: "a", ContentType: "image/png", SizeBytes: 10, Tags: []string{"x"}, CreatedAt: now},
		{ID: "b", ContentType: "video/mp4", SizeBytes: 20, CreatedAt: now.Add(-time.Hour)},
	}
	store := &adminDashboardMemeFakeStore{
		adminReadCacheStore: &adminReadCacheStore{memes: memes},
		leanMemes:           memes,
	}
	m := NewMemeManager(store)

	stats := m.AdminDashboard()

	if store.leanCalls != 1 {
		t.Fatalf("AdminDashboardMemes called %d times, want 1", store.leanCalls)
	}
	if store.adminReadCacheStore.callCount() != 0 {
		t.Fatalf("List() fallback used %d times, want 0", store.adminReadCacheStore.callCount())
	}
	if stats.Counts.Total != 2 || stats.TotalSizeBytes != 30 {
		t.Fatalf("stats not computed from lean scan: %+v", stats.Counts)
	}
}

func TestAdminDashboardFallsBackToListOnLeanScanError(t *testing.T) {
	now := time.Now().UTC()
	memes := []accessor.Meme{{ID: "a", ContentType: "image/png", SizeBytes: 7, CreatedAt: now}}
	store := &adminDashboardMemeFakeStore{
		adminReadCacheStore: &adminReadCacheStore{memes: memes},
		leanErr:             errors.New("boom"),
	}
	m := NewMemeManager(store)

	stats := m.AdminDashboard()

	if store.leanCalls != 1 {
		t.Fatalf("AdminDashboardMemes called %d times, want 1", store.leanCalls)
	}
	if store.adminReadCacheStore.callCount() != 1 {
		t.Fatalf("List() fallback used %d times, want 1", store.adminReadCacheStore.callCount())
	}
	if stats.Counts.Total != 1 || stats.TotalSizeBytes != 7 {
		t.Fatalf("fallback stats wrong: %+v total=%d", stats.Counts, stats.TotalSizeBytes)
	}
}
