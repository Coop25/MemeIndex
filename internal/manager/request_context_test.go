package manager

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"memeindex/internal/accessor"
	"memeindex/internal/accessor/accessortest"
	"memeindex/internal/tagsuggest"
)

// These tests cover #78 in the manager: every method hands its caller's
// context to the store, nothing computed on a cancelled context is cached for
// the next caller, and the multi-write operations finish once started.

type requestContextKey struct{}

func requestScoped(parent context.Context) context.Context {
	return context.WithValue(parent, requestContextKey{}, "request")
}

func cancelledRequest() context.Context {
	ctx, cancel := context.WithCancel(requestScoped(context.Background()))
	cancel()
	return ctx
}

// contextStore wraps the in-memory store and treats contexts the way the
// Postgres store does: a call made on a context that is done fails with the
// context's error instead of running. It also records every call made on a
// context that did not come from the caller (one without the request marker).
type contextStore struct {
	*accessortest.MemeStore

	mu     sync.Mutex
	missed []string
}

func newContextStore(t *testing.T) *contextStore {
	t.Helper()
	store, err := accessortest.NewMemeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &contextStore{MemeStore: store}
}

func (s *contextStore) check(name string, ctx context.Context) error {
	if ctx.Value(requestContextKey{}) != "request" {
		s.mu.Lock()
		s.missed = append(s.missed, name)
		s.mu.Unlock()
	}
	return ctx.Err()
}

func (s *contextStore) missedCalls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.missed...)
}

func (s *contextStore) List(ctx context.Context, userID, query string, favoritesOnly bool, tag string) []accessor.Meme {
	if s.check("List", ctx) != nil {
		return nil
	}
	return s.MemeStore.List(ctx, userID, query, favoritesOnly, tag)
}

func (s *contextStore) SuggestTags(ctx context.Context, prefix string, limit int) []string {
	if s.check("SuggestTags", ctx) != nil {
		return nil
	}
	return s.MemeStore.SuggestTags(ctx, prefix, limit)
}

func (s *contextStore) GetByID(ctx context.Context, userID, id string) (accessor.Meme, error) {
	if err := s.check("GetByID", ctx); err != nil {
		return accessor.Meme{}, err
	}
	return s.MemeStore.GetByID(ctx, userID, id)
}

func (s *contextStore) GetAnyByID(ctx context.Context, id string) (accessor.Meme, error) {
	if err := s.check("GetAnyByID", ctx); err != nil {
		return accessor.Meme{}, err
	}
	return s.MemeStore.GetAnyByID(ctx, id)
}

func (s *contextStore) MemesByIDs(ctx context.Context, ids []string) (map[string]accessor.Meme, error) {
	if err := s.check("MemesByIDs", ctx); err != nil {
		return nil, err
	}
	return s.MemeStore.MemesByIDs(ctx, ids)
}

func (s *contextStore) Random(ctx context.Context, excludedIDs []string) (accessor.Meme, error) {
	if err := s.check("Random", ctx); err != nil {
		return accessor.Meme{}, err
	}
	return s.MemeStore.Random(ctx, excludedIDs)
}

func (s *contextStore) Create(ctx context.Context, input accessor.CreateInput) (accessor.Meme, error) {
	if err := s.check("Create", ctx); err != nil {
		return accessor.Meme{}, err
	}
	return s.MemeStore.Create(ctx, input)
}

func (s *contextStore) EditMeme(ctx context.Context, userID, id string, edit accessor.MemeEdit) (accessor.Meme, error) {
	if err := s.check("EditMeme", ctx); err != nil {
		return accessor.Meme{}, err
	}
	return s.MemeStore.EditMeme(ctx, userID, id, edit)
}

func (s *contextStore) SetFavorite(ctx context.Context, userID, id string, favorite bool) (accessor.Meme, error) {
	if err := s.check("SetFavorite", ctx); err != nil {
		return accessor.Meme{}, err
	}
	return s.MemeStore.SetFavorite(ctx, userID, id, favorite)
}

func (s *contextStore) Delete(ctx context.Context, input accessor.DeleteInput) (accessor.DeleteResult, error) {
	if err := s.check("Delete", ctx); err != nil {
		return accessor.DeleteResult{}, err
	}
	return s.MemeStore.Delete(ctx, input)
}

func (s *contextStore) AdminDashboardMemes(ctx context.Context) ([]accessor.Meme, error) {
	if err := s.check("AdminDashboardMemes", ctx); err != nil {
		return nil, err
	}
	return s.MemeStore.AdminDashboardMemes(ctx)
}

func (s *contextStore) AllStoredFiles(ctx context.Context) ([]accessor.StoredFile, error) {
	if err := s.check("AllStoredFiles", ctx); err != nil {
		return nil, err
	}
	return s.MemeStore.AllStoredFiles(ctx)
}

func (s *contextStore) HiddenMemeIDs(ctx context.Context) ([]string, error) {
	if err := s.check("HiddenMemeIDs", ctx); err != nil {
		return nil, err
	}
	return s.MemeStore.HiddenMemeIDs(ctx)
}

func (s *contextStore) TotalFavoriteAssignments(ctx context.Context) (int, error) {
	if err := s.check("TotalFavoriteAssignments", ctx); err != nil {
		return 0, err
	}
	return s.MemeStore.TotalFavoriteAssignments(ctx)
}

func (s *contextStore) CountFavoritesByUser(ctx context.Context) (map[string]int, error) {
	if err := s.check("CountFavoritesByUser", ctx); err != nil {
		return nil, err
	}
	return s.MemeStore.CountFavoritesByUser(ctx)
}

func (s *contextStore) ListSuggestedTags(ctx context.Context, id string) ([]string, error) {
	if err := s.check("ListSuggestedTags", ctx); err != nil {
		return nil, err
	}
	return s.MemeStore.ListSuggestedTags(ctx, id)
}

func (s *contextStore) ReplaceSuggestedTags(ctx context.Context, id string, tags []string) error {
	if err := s.check("ReplaceSuggestedTags", ctx); err != nil {
		return err
	}
	return s.MemeStore.ReplaceSuggestedTags(ctx, id, tags)
}

func (s *contextStore) RemoveSuggestedTag(ctx context.Context, id, tag string) error {
	if err := s.check("RemoveSuggestedTag", ctx); err != nil {
		return err
	}
	return s.MemeStore.RemoveSuggestedTag(ctx, id, tag)
}

func (s *contextStore) SetAutoSuggestDisabled(ctx context.Context, id string, disabled bool) error {
	if err := s.check("SetAutoSuggestDisabled", ctx); err != nil {
		return err
	}
	return s.MemeStore.SetAutoSuggestDisabled(ctx, id, disabled)
}

func (s *contextStore) SetSearchText(ctx context.Context, id, text string) error {
	if err := s.check("SetSearchText", ctx); err != nil {
		return err
	}
	return s.MemeStore.SetSearchText(ctx, id, text)
}

func (s *contextStore) GetOrCreateMemeShare(ctx context.Context, memeID, userID string, now, expiresAt time.Time) (accessor.MemeShareState, error) {
	if err := s.check("GetOrCreateMemeShare", ctx); err != nil {
		return accessor.MemeShareState{}, err
	}
	return s.MemeStore.GetOrCreateMemeShare(ctx, memeID, userID, now, expiresAt)
}

func (s *contextStore) GetMemeShareState(ctx context.Context, memeID string) (accessor.MemeShareState, error) {
	if err := s.check("GetMemeShareState", ctx); err != nil {
		return accessor.MemeShareState{}, err
	}
	return s.MemeStore.GetMemeShareState(ctx, memeID)
}

func (s *contextStore) ResolveMemeShareSlug(ctx context.Context, slug string) (string, int64, error) {
	if err := s.check("ResolveMemeShareSlug", ctx); err != nil {
		return "", 0, err
	}
	return s.MemeStore.ResolveMemeShareSlug(ctx, slug)
}

func (s *contextStore) ListActiveMemeShares(ctx context.Context, now time.Time) ([]accessor.MemeShareState, error) {
	if err := s.check("ListActiveMemeShares", ctx); err != nil {
		return nil, err
	}
	return s.MemeStore.ListActiveMemeShares(ctx, now)
}

func (s *contextStore) RevokeMemeShare(ctx context.Context, memeID string) error {
	if err := s.check("RevokeMemeShare", ctx); err != nil {
		return err
	}
	return s.MemeStore.RevokeMemeShare(ctx, memeID)
}

func (s *contextStore) RevokeAllMemeShares(ctx context.Context, now time.Time) (int, error) {
	if err := s.check("RevokeAllMemeShares", ctx); err != nil {
		return 0, err
	}
	return s.MemeStore.RevokeAllMemeShares(ctx, now)
}

func createContextMeme(t *testing.T, m *MemeManager, name string, tags ...string) accessor.Meme {
	t.Helper()
	meme, err := m.CreateMeme(requestScoped(context.Background()),
		strings.NewReader("context test bytes "+name), textproto.MIMEHeader{"Content-Type": []string{"image/png"}},
		name+".png", tags, "")
	if err != nil {
		t.Fatalf("CreateMeme: %v", err)
	}
	return meme
}

// TestManagerPassesTheCallerContextToTheStore drives every manager method
// that reaches the store with a request-scoped context and checks that each
// store call it made was given that context.
func TestManagerPassesTheCallerContextToTheStore(t *testing.T) {
	store := newContextStore(t)
	m := NewMemeManager(store)
	ctx := requestScoped(context.Background())
	actor := accessor.AuditActor{UserID: "u-ctx", Username: "ctx"}
	now := time.Now()

	meme := createContextMeme(t, m, "first", "cat")
	other, err := m.CreateMemeAs(ctx, actor, strings.NewReader("second bytes"), textproto.MIMEHeader{"Content-Type": []string{"image/png"}}, "second.png", []string{"cats"}, "")
	if err != nil {
		t.Fatalf("CreateMemeAs: %v", err)
	}
	if err := store.MemeStore.ReplaceSuggestedTags(context.Background(), meme.ID, []string{"dog", "bird"}); err != nil {
		t.Fatal(err)
	}

	steps := []struct {
		name string
		run  func() error
	}{
		{"GetMeme", func() error { _, err := m.GetMeme(ctx, "u-ctx", meme.ID); return err }},
		{"GetAdminMeme", func() error { _, err := m.GetAdminMeme(ctx, meme.ID); return err }},
		{"EditMeme", func() error {
			_, err := m.EditMeme(ctx, "u-ctx", meme.ID, accessor.MemeEdit{TagsAdded: []string{"kitten"}})
			return err
		}},
		{"SetFavorite", func() error { _, err := m.SetFavorite(ctx, "u-ctx", meme.ID, true); return err }},
		{"SetFavoriteAs", func() error { _, err := m.SetFavoriteAs(ctx, "u-ctx", meme.ID, false, actor); return err }},
		{"SuggestTags", func() error { m.SuggestTags(ctx, "ca", 5); return nil }},
		{"RandomMeme", func() error { _, err := m.RandomMeme(ctx, nil); return err }},
		{"StepRandomReel", func() error {
			step, err := m.StepRandomReel(ctx, "", "next")
			if err != nil {
				return err
			}
			return m.DeleteRandomReelSession(ctx, step.SessionID)
		}},
		{"ListMemes", func() error { _, err := m.ListMemes(ctx, "u-ctx", "", false, "", "", 0, 10); return err }},
		{"Dashboard", func() error { _, err := m.Dashboard(ctx, "u-ctx"); return err }},
		{"PopularTags", func() error { _, err := m.PopularTags(ctx, "u-ctx", 5); return err }},
		{"Shares", func() error {
			share, err := m.GetOrCreateMemeShare(ctx, meme.ID, "u-ctx", now)
			if err != nil {
				return err
			}
			if _, _, err := m.GetMemeShare(ctx, meme.ID); err != nil {
				return err
			}
			if _, _, _, err := m.ResolveMemeShareLink(ctx, share.Slug); err != nil {
				return err
			}
			if _, err := m.ListActiveMemeShares(ctx, now); err != nil {
				return err
			}
			if _, _, _, _, err := m.ListActiveMemeSharesPage(ctx, now, 0, 10); err != nil {
				return err
			}
			if err := m.RevokeMemeShare(ctx, meme.ID); err != nil {
				return err
			}
			_, err = m.RevokeAllMemeShares(ctx, now)
			return err
		}},
		{"AdminDashboard", func() error { m.AdminDashboard(ctx); return nil }},
		{"StoredMemeFiles", func() error { _, err := m.StoredMemeFiles(ctx); return err }},
		{"CountFavoritesByUser", func() error { _, err := m.CountFavoritesByUser(ctx); return err }},
		{"IsMemeHidden", func() error { m.IsMemeHidden(ctx, meme.ID); return nil }},
		{"TagHygieneReport", func() error { m.TagHygieneReport(ctx); return nil }},
		{"TagHygienePage", func() error { m.TagHygienePage(ctx, TagHygienePageParams{}); return nil }},
		{"TagSuggestionQueueStatus", func() error { m.TagSuggestionQueueStatus(ctx, 0, 10); return nil }},
		{"GetStoredMemeTagSuggestions", func() error { _, err := m.GetStoredMemeTagSuggestions(ctx, meme.ID); return err }},
		{"ApplyMemeTagSuggestion", func() error {
			_, err := m.ApplyMemeTagSuggestion(ctx, "u-ctx", meme.ID, "dog", actor)
			return err
		}},
		{"DismissMemeTagSuggestion", func() error {
			_, err := m.DismissMemeTagSuggestion(ctx, "u-ctx", meme.ID, "bird")
			return err
		}},
		{"DismissAllMemeTagSuggestions", func() error {
			_, err := m.DismissAllMemeTagSuggestions(ctx, "u-ctx", meme.ID)
			return err
		}},
		{"MergeTags", func() error { _, err := m.MergeTags(ctx, "cats", "cat", actor); return err }},
		{"ResetTagSuggestionsAndRequeueUntagged", func() error {
			_, err := m.ResetTagSuggestionsAndRequeueUntagged(ctx, actor)
			return err
		}},
		{"DeleteMeme", func() error {
			_, err := m.DeleteMeme(ctx, other.ID, accessor.AuditActor{UserID: "u-ctx", IsSuperAdmin: true})
			return err
		}},
	}
	for _, step := range steps {
		if err := step.run(); err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
	}
	if missed := store.missedCalls(); len(missed) > 0 {
		t.Fatalf("store calls not made on the caller's context: %v", missed)
	}
}

// TestCancelledReadsAreNotCached checks the two admin read models the manager
// memoizes. Computed on a request that has gone away, their reads come back
// empty, and that result must not be served to the next caller.
func TestCancelledReadsAreNotCached(t *testing.T) {
	store := newContextStore(t)
	m := NewMemeManager(store)
	createContextMeme(t, m, "kept", "cat")

	if got := m.AdminDashboard(cancelledRequest()).Counts.Total; got != 0 {
		t.Fatalf("dashboard on a cancelled request counted %d memes; the store should have refused", got)
	}
	if got := m.AdminDashboard(requestScoped(context.Background())).Counts.Total; got != 1 {
		t.Fatalf("dashboard after a cancelled request counted %d memes, want 1 (the empty result was cached)", got)
	}

	if got := m.TagHygieneReport(cancelledRequest()).Tags; len(got) != 0 {
		t.Fatalf("tag hygiene on a cancelled request found tags %v; the store should have refused", got)
	}
	if got := m.TagHygieneReport(requestScoped(context.Background())).Tags; len(got) != 1 {
		t.Fatalf("tag hygiene after a cancelled request found %d tags, want 1 (the empty report was memoized)", len(got))
	}
}

// TestSuggestionResetFinishesDespiteCancelledContext checks that the admin
// reset, one write per meme and then an audit entry, runs to the end even if
// the request that started it has already been cancelled.
func TestSuggestionResetFinishesDespiteCancelledContext(t *testing.T) {
	store := newContextStore(t)
	m := NewMemeManager(store)
	meme := createContextMeme(t, m, "suggested")
	if err := store.MemeStore.ReplaceSuggestedTags(context.Background(), meme.ID, []string{"dog"}); err != nil {
		t.Fatal(err)
	}
	if err := store.MemeStore.SetAutoSuggestDisabled(context.Background(), meme.ID, true); err != nil {
		t.Fatal(err)
	}

	result, err := m.ResetTagSuggestionsAndRequeueUntagged(cancelledRequest(), accessor.AuditActor{UserID: "u-admin"})
	if err != nil {
		t.Fatalf("reset with a cancelled context: %v", err)
	}
	if result.ClearedSuggestions != 1 || result.ClearedExhausted != 1 {
		t.Fatalf("reset with a cancelled context = %+v, want one suggestion set and one exhausted mark cleared", result)
	}
	got, err := store.MemeStore.GetByID(context.Background(), "", meme.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.SuggestedTags) != 0 || got.AutoSuggestDisabled {
		t.Fatalf("meme after reset = suggestions %v, disabled %v; want both cleared", got.SuggestedTags, got.AutoSuggestDisabled)
	}
}

// endsWithTheAnswer is a request context that reports itself cancelled from
// the moment the model has answered, as a deadline falling just after the
// answer would. Done is never closed, so the HTTP call that brings the answer
// back still completes.
type endsWithTheAnswer struct {
	context.Context
	ended atomic.Bool
}

func (c *endsWithTheAnswer) Err() error {
	if c.ended.Load() {
		return context.Canceled
	}
	return nil
}

// TestSuggestionResultIsStoredWhenTheContextEndsWithIt checks that tag
// suggestions the model has already produced are recorded even though the
// context of the request (or worker deadline) that asked for them ended as
// they arrived.
func TestSuggestionResultIsStoredWhenTheContextEndsWithIt(t *testing.T) {
	ctx := &endsWithTheAnswer{Context: requestScoped(context.Background())}
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// End the context before answering, so nothing after the answer can
		// run on it while it is still live.
		ctx.ended.Store(true)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"response": `{"tags":["dog","bird"],"text":"woof"}`})
	}))
	defer ollama.Close()

	store := newContextStore(t)
	suggester := tagsuggest.New(tagsuggest.Config{OllamaURL: ollama.URL, Model: "test", Timeout: 5 * time.Second, GenerateOnly: true})
	m := NewMemeManagerWithTagSuggester(store, suggester, nil, TagSuggestionRuntimeConfig{}, 60)
	meme := createContextMeme(t, m, "to-suggest")

	result, err := m.RefreshMemeTagSuggestions(ctx, "", meme.ID)
	if err != nil {
		t.Fatalf("RefreshMemeTagSuggestions: %v", err)
	}
	if len(result.Tags) != 2 {
		t.Fatalf("suggested tags = %v, want dog and bird", result.Tags)
	}
	stored, err := store.MemeStore.ListSuggestedTags(context.Background(), meme.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 2 {
		t.Fatalf("stored suggestions = %v, want the two the model returned", stored)
	}
}

// leavesAfterEdit is a contextStore whose client goes away the moment an
// edit has been written, as a browser closed right after the tag was saved.
type leavesAfterEdit struct {
	*contextStore
	leave context.CancelFunc
}

func (s *leavesAfterEdit) EditMeme(ctx context.Context, userID, id string, edit accessor.MemeEdit) (accessor.Meme, error) {
	meme, err := s.contextStore.EditMeme(ctx, userID, id, edit)
	s.leave()
	return meme, err
}

// TestApplyingASuggestionFinishesWhenTheRequestEndsAfterTheTagIsAdded checks
// that applying a suggested tag also drops it from the suggestions when the
// request is cancelled right after the tag has been added. Stopping there
// would leave the tag both applied and still suggested.
func TestApplyingASuggestionFinishesWhenTheRequestEndsAfterTheTagIsAdded(t *testing.T) {
	ctx, cancel := context.WithCancel(requestScoped(context.Background()))
	defer cancel()
	store := &leavesAfterEdit{contextStore: newContextStore(t), leave: cancel}
	m := NewMemeManager(store)
	meme := createContextMeme(t, m, "to-apply")
	if err := store.MemeStore.ReplaceSuggestedTags(context.Background(), meme.ID, []string{"dog", "bird"}); err != nil {
		t.Fatal(err)
	}

	if _, err := m.ApplyMemeTagSuggestion(ctx, "", meme.ID, "dog", accessor.AuditActor{UserID: "u-ctx"}); err != nil {
		t.Fatalf("ApplyMemeTagSuggestion with the request ending after the edit: %v", err)
	}
	got, err := store.MemeStore.GetByID(context.Background(), "", meme.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(got.Tags, "dog") {
		t.Fatalf("tags = %v, want dog applied", got.Tags)
	}
	if slices.Contains(got.SuggestedTags, "dog") {
		t.Fatalf("suggestions = %v: dog was applied but is still suggested", got.SuggestedTags)
	}
}
