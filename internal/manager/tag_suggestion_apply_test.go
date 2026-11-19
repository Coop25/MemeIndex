package manager

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/textproto"
	"os"
	"strings"
	"testing"

	"memeindex/internal/accessor"
	"memeindex/internal/accessor/accessortest"
)

type suggestionTestStore interface {
	accessor.Store
	accessor.SuggestedTagStore
}

// notesRaceStore lands someone else's notes edit right after the first read
// of a meme, the window in which ApplyMemeTagSuggestion used to hold a stale
// snapshot that it then wrote back over the notes.
type notesRaceStore struct {
	suggestionTestStore
	fired bool
}

func (s *notesRaceStore) GetByID(userID, id string) (accessor.Meme, error) {
	meme, err := s.suggestionTestStore.GetByID(userID, id)
	if err == nil && !s.fired {
		s.fired = true
		notes := "edited by someone else"
		if _, editErr := s.EditMeme("u-other", id, accessor.MemeEdit{Notes: &notes}); editErr != nil {
			return accessor.Meme{}, editErr
		}
	}
	return meme, err
}

func TestApplyMemeTagSuggestionKeepsConcurrentNotesEdit(t *testing.T) {
	t.Run("memory", func(t *testing.T) {
		store, err := accessortest.NewMemeStore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		testApplySuggestionKeepsNotes(t, store)
	})
	t.Run("postgres", func(t *testing.T) {
		dsn := strings.TrimSpace(os.Getenv("MEMEINDEX_TEST_DATABASE_URL"))
		if dsn == "" {
			t.Skip("set MEMEINDEX_TEST_DATABASE_URL to run the Postgres integration test")
		}
		store, err := accessor.NewPostgresStore(context.Background(), dsn, t.TempDir())
		if err != nil {
			t.Fatalf("NewPostgresStore: %v", err)
		}
		t.Cleanup(store.Close)
		testApplySuggestionKeepsNotes(t, store)
	})
}

func testApplySuggestionKeepsNotes(t *testing.T, store suggestionTestStore) {
	t.Helper()
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	token := "apply" + hex.EncodeToString(b)
	meme, err := store.Create(accessor.CreateInput{
		File:        strings.NewReader("apply suggestion bytes " + token),
		Header:      textproto.MIMEHeader{"Content-Type": []string{"image/png"}},
		Filename:    token + ".png",
		ContentType: "image/png",
		Tags:        []string{token + "-cat"},
		Notes:       "original notes",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		_, _ = store.Delete(accessor.DeleteInput{ID: meme.ID, Actor: accessor.AuditActor{IsSuperAdmin: true}})
	})
	if err := store.ReplaceSuggestedTags(meme.ID, []string{token + "-dog", token + "-fish"}); err != nil {
		t.Fatalf("ReplaceSuggestedTags: %v", err)
	}

	m := NewMemeManager(&notesRaceStore{suggestionTestStore: store})
	if _, err := m.ApplyMemeTagSuggestion("u-apply", meme.ID, token+"-dog", accessor.AuditActor{UserID: "u-apply"}); err != nil {
		t.Fatalf("ApplyMemeTagSuggestion: %v", err)
	}

	got, err := store.GetByID("", meme.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Notes != "edited by someone else" {
		t.Fatalf("notes = %q: applying a suggestion overwrote a concurrent notes edit", got.Notes)
	}
	if want := token + "-cat," + token + "-dog"; strings.Join(got.Tags, ",") != want {
		t.Fatalf("tags = %v, want %s", got.Tags, want)
	}
	if want := token + "-fish"; strings.Join(got.SuggestedTags, ",") != want {
		t.Fatalf("suggested tags = %v, want %s", got.SuggestedTags, want)
	}
}
