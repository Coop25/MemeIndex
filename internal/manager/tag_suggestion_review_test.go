package manager

import (
	"fmt"
	"os"
	"testing"

	"memeindex/internal/accessor"
)

type tagReviewTestStore struct {
	memes []accessor.Meme
}

func (s *tagReviewTestStore) List(string, string, bool, string) []accessor.Meme {
	return append([]accessor.Meme(nil), s.memes...)
}
func (s *tagReviewTestStore) SuggestTags(string, int) []string { return nil }
func (s *tagReviewTestStore) GetByID(_ string, id string) (accessor.Meme, error) {
	for _, meme := range s.memes {
		if meme.ID == id {
			return meme, nil
		}
	}
	return accessor.Meme{}, os.ErrNotExist
}
func (s *tagReviewTestStore) Random([]string) (accessor.Meme, error) {
	return accessor.Meme{}, os.ErrNotExist
}
func (s *tagReviewTestStore) Create(accessor.CreateInput) (accessor.Meme, error) {
	return accessor.Meme{}, nil
}
func (s *tagReviewTestStore) Update(string, string, accessor.MemeUpdate) (accessor.Meme, error) {
	return accessor.Meme{}, nil
}
func (s *tagReviewTestStore) SetFavorite(string, string, bool) (accessor.Meme, error) {
	return accessor.Meme{}, nil
}
func (s *tagReviewTestStore) Delete(accessor.DeleteInput) (accessor.DeleteResult, error) {
	return accessor.DeleteResult{}, nil
}
func (s *tagReviewTestStore) UploadDir() string { return os.TempDir() }
func (s *tagReviewTestStore) ListSuggestedTags(id string) ([]string, error) {
	meme, err := s.GetByID("", id)
	return append([]string(nil), meme.SuggestedTags...), err
}
func (s *tagReviewTestStore) ReplaceSuggestedTags(id string, tags []string) error {
	for index := range s.memes {
		if s.memes[index].ID == id {
			s.memes[index].SuggestedTags = append([]string(nil), tags...)
			return nil
		}
	}
	return os.ErrNotExist
}
func (s *tagReviewTestStore) SetAutoSuggestDisabled(string, bool) error { return nil }

// Keep compile-time coverage explicit when the Store interface grows.
var _ accessor.Store = (*tagReviewTestStore)(nil)
var _ accessor.SuggestedTagStore = (*tagReviewTestStore)(nil)

func TestTagSuggestionQueueStatusPaginatesPendingReview(t *testing.T) {
	store := &tagReviewTestStore{}
	for index := 0; index < 125; index++ {
		store.memes = append(store.memes, accessor.Meme{
			ID:            fmt.Sprintf("meme-%03d", index),
			OriginalName:  fmt.Sprintf("Meme %03d", index),
			SuggestedTags: []string{"review-me"},
		})
	}

	manager := NewMemeManager(store)
	status := manager.TagSuggestionQueueStatus(50, 50)
	if status.PendingSuggestionMemes != 125 {
		t.Fatalf("pending total = %d, want 125", status.PendingSuggestionMemes)
	}
	if len(status.PendingReviewMemes) != 50 {
		t.Fatalf("page length = %d, want 50", len(status.PendingReviewMemes))
	}
	if status.PendingReviewMemes[0].ID != "meme-050" {
		t.Fatalf("first page item = %q, want meme-050", status.PendingReviewMemes[0].ID)
	}
	if !status.PendingReviewHasMore || status.PendingReviewNextOffset != 100 {
		t.Fatalf("pagination = has_more %v next %d, want true and 100", status.PendingReviewHasMore, status.PendingReviewNextOffset)
	}

	lastPage := manager.TagSuggestionQueueStatus(100, 50)
	if len(lastPage.PendingReviewMemes) != 25 || lastPage.PendingReviewHasMore {
		t.Fatalf("last page = %d items, has_more %v; want 25 and false", len(lastPage.PendingReviewMemes), lastPage.PendingReviewHasMore)
	}
}

func TestDismissAllMemeTagSuggestionsClearsReviewItem(t *testing.T) {
	store := &tagReviewTestStore{memes: []accessor.Meme{{
		ID:            "reviewed",
		SuggestedTags: []string{"one", "two"},
	}}}
	manager := NewMemeManager(store)

	meme, err := manager.DismissAllMemeTagSuggestions("", "reviewed")
	if err != nil {
		t.Fatalf("dismiss all: %v", err)
	}
	if len(meme.SuggestedTags) != 0 {
		t.Fatalf("suggested tags = %v, want empty", meme.SuggestedTags)
	}
}
