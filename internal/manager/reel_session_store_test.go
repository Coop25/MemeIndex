package manager

import (
	"fmt"
	"os"
	"testing"
	"time"

	"memeindex/internal/accessor"
)

type reelTestStore struct {
	memes        map[string]accessor.Meme
	lastExcluded []string
	randomPickID string
}

func (s *reelTestStore) List(userID, query string, favoritesOnly bool, tag string) []accessor.Meme {
	return nil
}

func (s *reelTestStore) SuggestTags(prefix string, limit int) []string {
	return nil
}

func (s *reelTestStore) GetByID(userID, id string) (accessor.Meme, error) {
	meme, ok := s.memes[id]
	if !ok {
		return accessor.Meme{}, os.ErrNotExist
	}
	return meme, nil
}

func (s *reelTestStore) Random(excludedIDs []string) (accessor.Meme, error) {
	s.lastExcluded = append([]string(nil), excludedIDs...)
	meme, ok := s.memes[s.randomPickID]
	if !ok {
		return accessor.Meme{}, os.ErrNotExist
	}
	return meme, nil
}

func (s *reelTestStore) Create(input accessor.CreateInput) (accessor.Meme, error) {
	return accessor.Meme{}, os.ErrNotExist
}

func (s *reelTestStore) Update(userID, id string, update accessor.MemeUpdate) (accessor.Meme, error) {
	return accessor.Meme{}, os.ErrNotExist
}

func (s *reelTestStore) SetFavorite(userID, id string, favorite bool) (accessor.Meme, error) {
	return accessor.Meme{}, os.ErrNotExist
}

func (s *reelTestStore) Delete(input accessor.DeleteInput) (accessor.DeleteResult, error) {
	return accessor.DeleteResult{}, os.ErrNotExist
}

func (s *reelTestStore) UploadDir() string {
	return "."
}

func TestRecentHistoryLockedCapsAtTwoHundred(t *testing.T) {
	store := &ReelSessionStore{}
	session := &reelSession{History: make([]string, 250)}
	for i := range session.History {
		session.History[i] = testMemeID(i)
	}

	recent := store.recentHistoryLocked(session)

	if len(recent) != 200 {
		t.Fatalf("recent history len = %d, want 200", len(recent))
	}
	if recent[0] != testMemeID(50) {
		t.Fatalf("recent history starts at %q, want %q", recent[0], testMemeID(50))
	}
	if recent[len(recent)-1] != testMemeID(249) {
		t.Fatalf("recent history ends at %q, want %q", recent[len(recent)-1], testMemeID(249))
	}
}

func TestStepNextExcludesLastTwoHundredBeforePicking(t *testing.T) {
	memes := map[string]accessor.Meme{
		"fresh": {ID: "fresh", OriginalName: "fresh"},
	}
	for i := 0; i < 250; i++ {
		id := testMemeID(i)
		memes[id] = accessor.Meme{ID: id, OriginalName: id}
	}

	mockStore := &reelTestStore{
		memes:        memes,
		randomPickID: "fresh",
	}
	store := &ReelSessionStore{
		sessions:    map[string]*reelSession{},
		store:       mockStore,
		sessionFile: "",
	}
	history := make([]string, 250)
	for i := range history {
		history[i] = testMemeID(i)
	}
	store.sessions["session-1"] = &reelSession{
		History:      history,
		Position:     len(history) - 1,
		LastActivity: time.Now().UTC(),
	}

	result, err := store.Step("session-1", "next")
	if err != nil {
		t.Fatalf("Step returned error: %v", err)
	}

	if result.Meme.ID != "fresh" {
		t.Fatalf("Step returned meme %q, want %q", result.Meme.ID, "fresh")
	}
	if len(mockStore.lastExcluded) != 200 {
		t.Fatalf("excluded len = %d, want 200", len(mockStore.lastExcluded))
	}
	if mockStore.lastExcluded[0] != testMemeID(50) {
		t.Fatalf("excluded starts at %q, want %q", mockStore.lastExcluded[0], testMemeID(50))
	}
	if mockStore.lastExcluded[len(mockStore.lastExcluded)-1] != testMemeID(249) {
		t.Fatalf("excluded ends at %q, want %q", mockStore.lastExcluded[len(mockStore.lastExcluded)-1], testMemeID(249))
	}
}

func testMemeID(index int) string {
	return fmt.Sprintf("meme-%03d", index)
}
