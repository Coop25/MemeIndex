package manager

import (
	"fmt"
	"testing"

	"memeindex/internal/accessor"
	"memeindex/internal/tagsuggest"
)

// stubTagSuggester returns an enabled Service that performs no I/O until its
// worker is started (which these tests never do).
func stubTagSuggester() *tagsuggest.Service {
	return tagsuggest.New(tagsuggest.Config{OllamaURL: "http://127.0.0.1:0", Model: "stub"})
}

// tagOpsFakeStore adds the tag-maintenance / tag-suggestion-query / tag-usage
// capabilities on top of the in-memory fake so the manager's SQL-delegation
// paths are exercised without a database. adminReadCacheStore.callCount() still
// reports whether the whole-archive scan fallback was used.
type tagOpsFakeStore struct {
	*adminReadCacheStore

	mergeCalls    int
	mergeSource   string
	mergeTarget   string
	mergeAffected int

	untagged      int
	untaggedIDs   []string
	pendingTotal  int
	pendingMemes  []accessor.Meme
	pendingOffset int
	pendingLimit  int

	usage map[string]int
}

func (s *tagOpsFakeStore) MergeTag(sourceTag, targetTag string, _ accessor.AuditActor) (int, error) {
	s.mergeCalls++
	s.mergeSource, s.mergeTarget = sourceTag, targetTag
	return s.mergeAffected, nil
}

func (s *tagOpsFakeStore) UntaggedWithoutSuggestionsCount() (int, error) {
	return s.untagged, nil
}

func (s *tagOpsFakeStore) UntaggedWithoutSuggestionIDs() ([]string, error) {
	return append([]string(nil), s.untaggedIDs...), nil
}

func (s *tagOpsFakeStore) PendingSuggestionMemes(offset, limit int) (int, []accessor.Meme, error) {
	s.pendingOffset, s.pendingLimit = offset, limit
	return s.pendingTotal, append([]accessor.Meme(nil), s.pendingMemes...), nil
}

func (s *tagOpsFakeStore) TagUsageCounts() (map[string]int, error) {
	out := map[string]int{}
	for k, v := range s.usage {
		out[k] = v
	}
	return out, nil
}

var (
	_ accessor.TagMaintenanceStore     = (*tagOpsFakeStore)(nil)
	_ accessor.TagSuggestionQueryStore = (*tagOpsFakeStore)(nil)
	_ accessor.TagUsageStore           = (*tagOpsFakeStore)(nil)
)

func newTagOpsFakeStore() *tagOpsFakeStore {
	return &tagOpsFakeStore{adminReadCacheStore: &adminReadCacheStore{}}
}

func TestMergeTagsDelegatesToStore(t *testing.T) {
	store := newTagOpsFakeStore()
	store.mergeAffected = 4
	m := NewMemeManager(store)

	result, err := m.MergeTags("  Buisness ", "BUSINESS", accessor.AuditActor{UserID: "admin"})
	if err != nil {
		t.Fatalf("MergeTags: %v", err)
	}

	if store.mergeCalls != 1 {
		t.Fatalf("MergeTag called %d times, want 1", store.mergeCalls)
	}
	if store.mergeSource != "buisness" || store.mergeTarget != "business" {
		t.Fatalf("merge args = %q -> %q, want normalized buisness -> business", store.mergeSource, store.mergeTarget)
	}
	if store.callCount() != 0 {
		t.Fatalf("whole-archive scan was used %d times, want 0", store.callCount())
	}
	if result.AffectedMemes != 4 || result.SourceTag != "buisness" || result.TargetTag != "business" {
		t.Fatalf("result = %+v", result)
	}
}

func TestTagSuggestionQueueStatusDelegatesToStore(t *testing.T) {
	store := newTagOpsFakeStore()
	store.untagged = 7
	store.pendingTotal = 125
	for i := 0; i < 50; i++ {
		store.pendingMemes = append(store.pendingMemes, accessor.Meme{
			ID:            fmt.Sprintf("meme-%03d", 50+i),
			OriginalName:  fmt.Sprintf("Meme %03d", 50+i),
			SuggestedTags: []string{"review-me"},
		})
	}
	m := NewMemeManager(store)

	status := m.TagSuggestionQueueStatus(50, 50)

	if store.callCount() != 0 {
		t.Fatalf("whole-archive scan was used %d times, want 0", store.callCount())
	}
	if store.pendingOffset != 50 || store.pendingLimit != 50 {
		t.Fatalf("pending page requested offset=%d limit=%d, want 50/50", store.pendingOffset, store.pendingLimit)
	}
	if status.UntaggedWithoutSuggestions != 7 {
		t.Fatalf("untagged = %d, want 7", status.UntaggedWithoutSuggestions)
	}
	if status.PendingSuggestionMemes != 125 || len(status.PendingReviewMemes) != 50 {
		t.Fatalf("pending total=%d page=%d, want 125 and 50", status.PendingSuggestionMemes, len(status.PendingReviewMemes))
	}
	if status.PendingReviewMemes[0].ID != "meme-050" {
		t.Fatalf("first review item = %q, want meme-050", status.PendingReviewMemes[0].ID)
	}
	if !status.PendingReviewHasMore || status.PendingReviewNextOffset != 100 {
		t.Fatalf("pagination = has_more %v next %d, want true and 100", status.PendingReviewHasMore, status.PendingReviewNextOffset)
	}
}

func TestSeedTagSuggestionQueueUsesStoreIDs(t *testing.T) {
	store := newTagOpsFakeStore()
	store.untaggedIDs = []string{"a", "b", "c"}
	m := NewMemeManagerWithTagSuggester(store, stubTagSuggester(), nil, TagSuggestionRuntimeConfig{}, 60)

	queued := m.SeedTagSuggestionQueue()

	if store.callCount() != 0 {
		t.Fatalf("whole-archive scan was used %d times, want 0", store.callCount())
	}
	if queued != 3 {
		t.Fatalf("queued = %d, want 3", queued)
	}
}

func TestTagHygieneReportUsesTagUsageStore(t *testing.T) {
	store := newTagOpsFakeStore()
	store.usage = map[string]int{"buisness": 1, "business": 9}
	m := NewMemeManager(store)

	report := m.TagHygieneReport()

	if store.callCount() != 0 {
		t.Fatalf("whole-archive scan was used %d times, want 0", store.callCount())
	}
	counts := map[string]int{}
	for _, tag := range report.Tags {
		counts[tag.Tag] = tag.Count
	}
	if counts["business"] != 9 || counts["buisness"] != 1 {
		t.Fatalf("tag counts not carried from TagUsageCounts: %+v", counts)
	}
	if len(report.Pairs) == 0 {
		t.Fatal("expected the near-duplicate pair to still be detected")
	}
}
