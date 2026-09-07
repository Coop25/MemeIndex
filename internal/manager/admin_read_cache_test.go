package manager

import (
	"os"
	"sync"
	"testing"
	"time"

	"memeindex/internal/accessor"
)

type adminReadCacheStore struct {
	mu         sync.Mutex
	memes      []accessor.Meme
	listCalls  int
	auditCalls []recordedSystemAudit
}

type recordedSystemAudit struct {
	action      string
	actor       accessor.AuditActor
	description string
}

func (s *adminReadCacheStore) List(string, string, bool, string) []accessor.Meme {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listCalls++
	return append([]accessor.Meme(nil), s.memes...)
}
func (s *adminReadCacheStore) SuggestTags(string, int) []string { return nil }
func (s *adminReadCacheStore) GetByID(_ string, id string) (accessor.Meme, error) {
	for _, meme := range s.memes {
		if meme.ID == id {
			return meme, nil
		}
	}
	return accessor.Meme{}, os.ErrNotExist
}
func (s *adminReadCacheStore) Random([]string) (accessor.Meme, error) {
	return accessor.Meme{}, os.ErrNotExist
}
func (s *adminReadCacheStore) Create(accessor.CreateInput) (accessor.Meme, error) {
	return accessor.Meme{}, nil
}
func (s *adminReadCacheStore) Update(string, string, accessor.MemeUpdate) (accessor.Meme, error) {
	return accessor.Meme{}, nil
}
func (s *adminReadCacheStore) SetFavorite(string, string, bool) (accessor.Meme, error) {
	return accessor.Meme{}, nil
}
func (s *adminReadCacheStore) Delete(accessor.DeleteInput) (accessor.DeleteResult, error) {
	return accessor.DeleteResult{}, nil
}
func (s *adminReadCacheStore) UploadDir() string { return os.TempDir() }

func (s *adminReadCacheStore) ListSuggestedTags(id string) ([]string, error) {
	meme, err := s.GetByID("", id)
	return append([]string(nil), meme.SuggestedTags...), err
}
func (s *adminReadCacheStore) ReplaceSuggestedTags(id string, tags []string) error {
	for index := range s.memes {
		if s.memes[index].ID == id {
			s.memes[index].SuggestedTags = append([]string(nil), tags...)
			return nil
		}
	}
	return os.ErrNotExist
}
func (s *adminReadCacheStore) SetAutoSuggestDisabled(string, bool) error { return nil }

func (s *adminReadCacheStore) RecordSystemAudit(action string, actor accessor.AuditActor, description string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.auditCalls = append(s.auditCalls, recordedSystemAudit{action: action, actor: actor, description: description})
	return nil
}

func (s *adminReadCacheStore) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listCalls
}

var (
	_ accessor.Store            = (*adminReadCacheStore)(nil)
	_ accessor.SuggestedTagStore = (*adminReadCacheStore)(nil)
	_ accessor.SystemAuditStore  = (*adminReadCacheStore)(nil)
)

func TestAdminDashboardMemoizesWithinTTL(t *testing.T) {
	store := &adminReadCacheStore{memes: []accessor.Meme{
		{ID: "a", SizeBytes: 10, Tags: []string{"x"}, CreatedAt: time.Now().UTC()},
	}}
	manager := NewMemeManager(store)

	first := manager.AdminDashboard()
	manager.AdminDashboard()
	if store.callCount() != 1 {
		t.Fatalf("store.List called %d times, want 1 (second read should be memoized)", store.callCount())
	}

	// A cache hit must be an independent copy: mutating it must not corrupt the
	// value handed to the next caller.
	if len(first.RecentMemes) > 0 {
		first.RecentMemes[0].FilePath = "mutated"
	}
	third := manager.AdminDashboard()
	if len(third.RecentMemes) == 0 || third.RecentMemes[0].FilePath == "mutated" {
		t.Fatalf("cache returned a shared slice; third read saw mutation: %+v", third.RecentMemes)
	}

	manager.invalidateAdminReadCache()
	manager.AdminDashboard()
	if store.callCount() != 2 {
		t.Fatalf("store.List called %d times after invalidation, want 2", store.callCount())
	}
}

func TestTagHygieneReportMemoizedAndInvalidatedByMerge(t *testing.T) {
	store := &adminReadCacheStore{memes: []accessor.Meme{
		{ID: "a", Tags: []string{"buisness"}, CreatedAt: time.Now().UTC()},
		{ID: "b", Tags: []string{"business"}, CreatedAt: time.Now().UTC()},
	}}
	manager := NewMemeManager(store)

	manager.TagHygieneReport()
	manager.TagHygieneReport()
	if store.callCount() != 1 {
		t.Fatalf("TagHygieneReport scanned the store %d times, want 1", store.callCount())
	}

	if _, err := manager.MergeTags("buisness", "business", accessor.AuditActor{UserID: "admin"}); err != nil {
		t.Fatalf("MergeTags: %v", err)
	}
	before := store.callCount()
	manager.TagHygieneReport()
	if store.callCount() == before {
		t.Fatalf("TagHygieneReport still memoized after a tag merge; expected a fresh scan")
	}
}

func TestResetTagSuggestionsRecordsSystemAudit(t *testing.T) {
	store := &adminReadCacheStore{memes: []accessor.Meme{
		{ID: "a", SuggestedTags: []string{"cat"}, CreatedAt: time.Now().UTC()},
		{ID: "b", CreatedAt: time.Now().UTC()},
	}}
	manager := NewMemeManager(store)

	actor := accessor.AuditActor{UserID: "99", Username: "root", DisplayName: "Root Admin"}
	if _, err := manager.ResetTagSuggestionsAndRequeueUntagged(actor); err != nil {
		t.Fatalf("ResetTagSuggestionsAndRequeueUntagged: %v", err)
	}

	if len(store.auditCalls) != 1 {
		t.Fatalf("recorded %d system audit entries, want 1", len(store.auditCalls))
	}
	entry := store.auditCalls[0]
	if entry.action != "tag_suggestions_reset" {
		t.Errorf("audit action = %q, want tag_suggestions_reset", entry.action)
	}
	if entry.actor.UserID != "99" || entry.actor.Username != "root" {
		t.Errorf("audit actor = %+v, want the caller", entry.actor)
	}
	if entry.description == "" {
		t.Errorf("audit description is empty")
	}
}
