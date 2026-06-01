package manager

import (
	"fmt"
	"os"
	"testing"
	"time"

	"memeindex/internal/accessor"
)

type reelTestStore struct {
	memes         map[string]accessor.Meme
	lastExcluded  []string
	allExcluded   [][]string
	randomPickID  string
	randomPickIDs []string
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
	s.allExcluded = append(s.allExcluded, append([]string(nil), excludedIDs...))
	pickID := s.randomPickID
	if len(s.randomPickIDs) > 0 {
		pickID = s.randomPickIDs[0]
		s.randomPickIDs = s.randomPickIDs[1:]
	}
	meme, ok := s.memes[pickID]
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
		"fresh":   {ID: "fresh", OriginalName: "fresh"},
		"ahead-1": {ID: "ahead-1", OriginalName: "ahead-1"},
		"ahead-2": {ID: "ahead-2", OriginalName: "ahead-2"},
	}
	for i := 0; i < 250; i++ {
		id := testMemeID(i)
		memes[id] = accessor.Meme{ID: id, OriginalName: id}
	}

	mockStore := &reelTestStore{
		memes:         memes,
		randomPickID:  "fresh",
		randomPickIDs: []string{"fresh", "ahead-1", "ahead-2"},
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
	if len(result.NextMemes) != 2 {
		t.Fatalf("next memes len = %d, want 2", len(result.NextMemes))
	}
	if result.NextMemes[0].ID != "ahead-1" || result.NextMemes[1].ID != "ahead-2" {
		t.Fatalf("next meme ids = %q, %q", result.NextMemes[0].ID, result.NextMemes[1].ID)
	}
	if len(mockStore.allExcluded) == 0 {
		t.Fatal("expected Random to be called at least once")
	}
	firstExcluded := mockStore.allExcluded[0]
	if len(firstExcluded) != 200 {
		t.Fatalf("excluded len = %d, want 200", len(firstExcluded))
	}
	if firstExcluded[0] != testMemeID(50) {
		t.Fatalf("excluded starts at %q, want %q", firstExcluded[0], testMemeID(50))
	}
	if firstExcluded[len(firstExcluded)-1] != testMemeID(249) {
		t.Fatalf("excluded ends at %q, want %q", firstExcluded[len(firstExcluded)-1], testMemeID(249))
	}
}

func TestStepPrevIncludesRecentHistoryForPreload(t *testing.T) {
	memes := map[string]accessor.Meme{}
	history := make([]string, 6)
	for i := range history {
		id := testMemeID(i)
		history[i] = id
		memes[id] = accessor.Meme{ID: id, OriginalName: id}
	}

	store := &ReelSessionStore{
		sessions: map[string]*reelSession{
			"session-1": {
				History:      history,
				Position:     4,
				LastActivity: time.Now().UTC(),
			},
		},
		store:       &reelTestStore{memes: memes},
		sessionFile: "",
	}

	result, err := store.Step("session-1", "prev")
	if err != nil {
		t.Fatalf("Step returned error: %v", err)
	}

	if result.Meme.ID != testMemeID(3) {
		t.Fatalf("Step returned meme %q, want %q", result.Meme.ID, testMemeID(3))
	}
	if len(result.PrevMemes) != 2 {
		t.Fatalf("prev memes len = %d, want 2", len(result.PrevMemes))
	}
	if result.PrevMemes[0].ID != testMemeID(1) || result.PrevMemes[1].ID != testMemeID(2) {
		t.Fatalf("prev meme ids = %q, %q", result.PrevMemes[0].ID, result.PrevMemes[1].ID)
	}
	if len(result.NextMemes) != 2 {
		t.Fatalf("next memes len = %d, want 2", len(result.NextMemes))
	}
	if result.NextMemes[0].ID != testMemeID(4) || result.NextMemes[1].ID != testMemeID(5) {
		t.Fatalf("next meme ids = %q, %q", result.NextMemes[0].ID, result.NextMemes[1].ID)
	}
}

func testMemeID(index int) string {
	return fmt.Sprintf("meme-%03d", index)
}
