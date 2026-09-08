package manager

import (
	"sync"
	"testing"
	"time"

	"memeindex/internal/accessor"
)

type searchTextFakeStore struct {
	*adminReadCacheStore

	mu    sync.Mutex
	calls map[string]string
}

func (s *searchTextFakeStore) SetSearchText(id, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.calls == nil {
		s.calls = map[string]string{}
	}
	s.calls[id] = text
	return nil
}

var _ accessor.SearchTextStore = (*searchTextFakeStore)(nil)

func TestResetTagSuggestionsClearsSearchTextForSuggestedMemes(t *testing.T) {
	store := &searchTextFakeStore{adminReadCacheStore: &adminReadCacheStore{memes: []accessor.Meme{
		{ID: "with-suggestions", SuggestedTags: []string{"cat"}, Tags: []string{"cat"}, CreatedAt: time.Now().UTC()},
		{ID: "plain", Tags: []string{"manual"}, CreatedAt: time.Now().UTC()},
	}}}
	m := NewMemeManager(store)

	if _, err := m.ResetTagSuggestionsAndRequeueUntagged(accessor.AuditActor{UserID: "admin"}); err != nil {
		t.Fatalf("reset: %v", err)
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if got, ok := store.calls["with-suggestions"]; !ok || got != "" {
		t.Fatalf("expected search text cleared for the suggested meme, calls=%v", store.calls)
	}
	if _, ok := store.calls["plain"]; ok {
		t.Fatalf("did not expect a search-text write for a meme without model suggestions, calls=%v", store.calls)
	}
}
