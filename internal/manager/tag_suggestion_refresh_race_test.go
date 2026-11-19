package manager

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/textproto"
	"os"
	"slices"
	"strings"
	"testing"

	"memeindex/internal/accessor"
	"memeindex/internal/accessor/accessortest"
)

// refreshRaceStore stores a fresh suggestion list - what the suggestion
// worker or a manual "refresh suggestions" writes - right after the read that
// apply or dismiss works from: after the tag edit for apply, which returns
// the meme it then used, and after the first GetByID for dismiss. That is the
// window in which both used to hold a stale list that they wrote back over
// the refresh.
type refreshRaceStore struct {
	suggestionTestStore
	afterEdit bool
	refresh   []string
	fired     bool
}

func (s *refreshRaceStore) land(id string) error {
	if s.fired {
		return nil
	}
	s.fired = true
	return s.ReplaceSuggestedTags(context.Background(), id, s.refresh)
}

func (s *refreshRaceStore) EditMeme(ctx context.Context, userID, id string, edit accessor.MemeEdit) (accessor.Meme, error) {
	meme, err := s.suggestionTestStore.EditMeme(ctx, userID, id, edit)
	if err == nil && s.afterEdit {
		if err := s.land(id); err != nil {
			return accessor.Meme{}, err
		}
	}
	return meme, err
}

func (s *refreshRaceStore) GetByID(ctx context.Context, userID, id string) (accessor.Meme, error) {
	meme, err := s.suggestionTestStore.GetByID(ctx, userID, id)
	if err == nil && !s.afterEdit {
		if err := s.land(id); err != nil {
			return accessor.Meme{}, err
		}
	}
	return meme, err
}

func TestSuggestionRefreshSurvivesApplyAndDismiss(t *testing.T) {
	stores := map[string]func(t *testing.T) suggestionTestStore{
		"memory": func(t *testing.T) suggestionTestStore {
			store, err := accessortest.NewMemeStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			return store
		},
		"postgres": func(t *testing.T) suggestionTestStore {
			dsn := strings.TrimSpace(os.Getenv("MEMEINDEX_TEST_DATABASE_URL"))
			if dsn == "" {
				t.Skip("set MEMEINDEX_TEST_DATABASE_URL to run the Postgres integration test")
			}
			store, err := accessor.NewPostgresStore(context.Background(), dsn, t.TempDir())
			if err != nil {
				t.Fatalf("NewPostgresStore: %v", err)
			}
			t.Cleanup(store.Close)
			return store
		},
	}
	for name, open := range stores {
		t.Run(name+"/apply", func(t *testing.T) {
			testSuggestionRefreshSurvives(t, open(t), true)
		})
		t.Run(name+"/dismiss", func(t *testing.T) {
			testSuggestionRefreshSurvives(t, open(t), false)
		})
	}
}

func testSuggestionRefreshSurvives(t *testing.T, store suggestionTestStore, apply bool) {
	t.Helper()
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	token := "refresh" + hex.EncodeToString(b)
	meme, err := store.Create(context.Background(), accessor.CreateInput{
		File:        strings.NewReader("refresh race bytes " + token),
		Header:      textproto.MIMEHeader{"Content-Type": []string{"image/png"}},
		Filename:    token + ".png",
		ContentType: "image/png",
		Tags:        []string{token + "-cat"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		_, _ = store.Delete(context.Background(), accessor.DeleteInput{ID: meme.ID, Actor: accessor.AuditActor{IsSuperAdmin: true}})
	})
	if err := store.ReplaceSuggestedTags(context.Background(), meme.ID, []string{token + "-dog", token + "-fish"}); err != nil {
		t.Fatalf("ReplaceSuggestedTags: %v", err)
	}

	// The refresh keeps the tag being applied or dismissed (so removing it
	// still has to happen) and replaces the other one.
	refreshed := []string{token + "-bird", token + "-dog"}
	m := NewMemeManager(&refreshRaceStore{suggestionTestStore: store, afterEdit: apply, refresh: refreshed})
	if apply {
		_, err = m.ApplyMemeTagSuggestion(context.Background(), "u-apply", meme.ID, token+"-dog", accessor.AuditActor{UserID: "u-apply"})
	} else {
		_, err = m.DismissMemeTagSuggestion(context.Background(), "u-dismiss", meme.ID, token+"-dog")
	}
	if err != nil {
		t.Fatalf("apply=%v: %v", apply, err)
	}

	got, err := store.GetByID(context.Background(), "", meme.ID)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{token + "-bird"}; !slices.Equal(got.SuggestedTags, want) {
		t.Fatalf("suggested tags = %v, want %v: the refresh was overwritten", got.SuggestedTags, want)
	}
	wantTags := []string{token + "-cat"}
	if apply {
		wantTags = append(wantTags, token+"-dog")
	}
	if !slices.Equal(got.Tags, wantTags) {
		t.Fatalf("tags = %v, want %v", got.Tags, wantTags)
	}
}
