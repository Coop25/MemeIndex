package client

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"memeindex/internal/accessor"
	"memeindex/internal/accessor/accessortest"
	"memeindex/internal/audiobooks"
	"memeindex/internal/manager"
	"memeindex/internal/tagsuggest"
)

// contextMemeStore wraps the in-memory meme store and treats contexts the way
// the Postgres store does: a call made on a context that is done fails with
// the context's error. It records calls made on a context that did not come
// from the request (one without the marker), and keeps reel sessions so the
// manager has a backend to load them from.
type contextMemeStore struct {
	*accessortest.MemeStore

	mu     sync.Mutex
	missed []string
	reels  map[string]accessor.ReelSessionRecord
}

func newContextMemeStore(t *testing.T) *contextMemeStore {
	t.Helper()
	store, err := accessortest.NewMemeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &contextMemeStore{MemeStore: store, reels: map[string]accessor.ReelSessionRecord{}}
}

func (s *contextMemeStore) check(name string, ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !fromRequest(ctx) {
		s.missed = append(s.missed, name)
	}
	return ctx.Err()
}

func (s *contextMemeStore) resetMissed() {
	s.mu.Lock()
	s.missed = nil
	s.mu.Unlock()
}

func (s *contextMemeStore) missedCalls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.missed...)
}

func (s *contextMemeStore) GetByID(ctx context.Context, userID, id string) (accessor.Meme, error) {
	if err := s.check("GetByID", ctx); err != nil {
		return accessor.Meme{}, err
	}
	return s.MemeStore.GetByID(ctx, userID, id)
}

func (s *contextMemeStore) Random(ctx context.Context, excludedIDs []string) (accessor.Meme, error) {
	if err := s.check("Random", ctx); err != nil {
		return accessor.Meme{}, err
	}
	return s.MemeStore.Random(ctx, excludedIDs)
}

func (s *contextMemeStore) Create(ctx context.Context, input accessor.CreateInput) (accessor.Meme, error) {
	if err := s.check("Create", ctx); err != nil {
		return accessor.Meme{}, err
	}
	return s.MemeStore.Create(ctx, input)
}

func (s *contextMemeStore) EditMeme(ctx context.Context, userID, id string, edit accessor.MemeEdit) (accessor.Meme, error) {
	if err := s.check("EditMeme", ctx); err != nil {
		return accessor.Meme{}, err
	}
	return s.MemeStore.EditMeme(ctx, userID, id, edit)
}

func (s *contextMemeStore) SetFavorite(ctx context.Context, userID, id string, favorite bool) (accessor.Meme, error) {
	if err := s.check("SetFavorite", ctx); err != nil {
		return accessor.Meme{}, err
	}
	return s.MemeStore.SetFavorite(ctx, userID, id, favorite)
}

func (s *contextMemeStore) Delete(ctx context.Context, input accessor.DeleteInput) (accessor.DeleteResult, error) {
	if err := s.check("Delete", ctx); err != nil {
		return accessor.DeleteResult{}, err
	}
	return s.MemeStore.Delete(ctx, input)
}

func (s *contextMemeStore) AdminDashboardMemes(ctx context.Context) ([]accessor.Meme, error) {
	if err := s.check("AdminDashboardMemes", ctx); err != nil {
		return nil, err
	}
	return s.MemeStore.AdminDashboardMemes(ctx)
}

func (s *contextMemeStore) AllStoredFiles(ctx context.Context) ([]accessor.StoredFile, error) {
	if err := s.check("AllStoredFiles", ctx); err != nil {
		return nil, err
	}
	return s.MemeStore.AllStoredFiles(ctx)
}

func (s *contextMemeStore) GetOrCreateMemeShare(ctx context.Context, memeID, userID string, now, expiresAt time.Time) (accessor.MemeShareState, error) {
	if err := s.check("GetOrCreateMemeShare", ctx); err != nil {
		return accessor.MemeShareState{}, err
	}
	return s.MemeStore.GetOrCreateMemeShare(ctx, memeID, userID, now, expiresAt)
}

func (s *contextMemeStore) GetMemeShareState(ctx context.Context, memeID string) (accessor.MemeShareState, error) {
	if err := s.check("GetMemeShareState", ctx); err != nil {
		return accessor.MemeShareState{}, err
	}
	return s.MemeStore.GetMemeShareState(ctx, memeID)
}

func (s *contextMemeStore) LoadReelSessions(ctx context.Context) (map[string]accessor.ReelSessionRecord, error) {
	if err := s.check("LoadReelSessions", ctx); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]accessor.ReelSessionRecord, len(s.reels))
	for id, record := range s.reels {
		out[id] = record
	}
	return out, nil
}

func (s *contextMemeStore) SaveReelSession(ctx context.Context, id string, record accessor.ReelSessionRecord) error {
	if err := s.check("SaveReelSession", ctx); err != nil {
		return err
	}
	s.mu.Lock()
	s.reels[id] = record
	s.mu.Unlock()
	return nil
}

func (s *contextMemeStore) DeleteReelSession(ctx context.Context, id string) error {
	if err := s.check("DeleteReelSession", ctx); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.reels, id)
	s.mu.Unlock()
	return nil
}

func (s *contextMemeStore) CleanupStaleReelSessions(ctx context.Context, _ time.Time) error {
	return s.check("CleanupStaleReelSessions", ctx)
}

func (s *contextMemeStore) createDirect(t *testing.T, name string) accessor.Meme {
	t.Helper()
	meme, err := s.MemeStore.Create(context.Background(), accessor.CreateInput{
		File:        strings.NewReader("request context bytes " + name),
		Header:      textproto.MIMEHeader{"Content-Type": []string{"image/png"}},
		Filename:    name + ".png",
		ContentType: "image/png",
	})
	if err != nil {
		t.Fatal(err)
	}
	return meme
}

// requestFor builds a request for a handler, carrying a session with every
// permission and the request-context marker, and cancelled first if asked.
func requestFor(method, target string, body io.Reader, cancelled bool) *http.Request {
	r := httptest.NewRequest(method, target, body)
	session := authSession{UserID: "u-ctx", Permissions: authPermissions{
		CanView: true, CanUpload: true, CanAddTags: true, CanRemoveTags: true,
		CanDeleteMemes: true, CanManageUsers: true, CanAdd: true, CanManage: true,
	}}
	ctx := requestScoped(contextWithSession(r.Context(), session))
	if cancelled {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		cancel()
	}
	return r.WithContext(ctx)
}

func newContextTestServer(t *testing.T, suggester *tagsuggest.Service) (*Server, *contextMemeStore) {
	t.Helper()
	store := newContextMemeStore(t)
	m := manager.NewMemeManagerWithTagSuggester(store, suggester, nil, manager.TagSuggestionRuntimeConfig{}, 60)
	store.resetMissed() // loading reel sessions at construction is not a request
	return &Server{managers: m, shareSecret: []byte("test-share-secret")}, store
}

// TestHandlersPassTheRequestContext runs the meme handlers on a marked
// request and checks every store call they caused was given that request's
// context, or one derived from it.
func TestHandlersPassTheRequestContext(t *testing.T) {
	s, store := newContextTestServer(t, nil)
	meme := store.createDirect(t, "handled")
	victim := store.createDirect(t, "deleted")

	steps := []struct {
		name string
		run  func(w http.ResponseWriter)
	}{
		{"get", func(w http.ResponseWriter) {
			s.handleMemeByID(w, requestFor(http.MethodGet, "/api/memes/"+meme.ID, nil, false))
		}},
		{"patch", func(w http.ResponseWriter) {
			s.handleMemeByID(w, requestFor(http.MethodPatch, "/api/memes/"+meme.ID, strings.NewReader(`{"notes":"new notes"}`), false))
		}},
		{"favorite", func(w http.ResponseWriter) {
			s.updateFavorite(w, requestFor(http.MethodPost, "/api/memes/"+meme.ID+"/favorite", strings.NewReader(`{"favorite":true}`), false), meme.ID)
		}},
		{"share", func(w http.ResponseWriter) {
			s.createMemeShare(w, requestFor(http.MethodPost, "/api/memes/"+meme.ID+"/share", nil, false), meme.ID)
		}},
		{"reel", func(w http.ResponseWriter) {
			s.handleRandomMeme(w, requestFor(http.MethodGet, "/api/memes/random?direction=next", nil, false))
		}},
		{"delete", func(w http.ResponseWriter) {
			s.deleteMeme(w, requestFor(http.MethodDelete, "/api/memes/"+victim.ID, nil, false), victim.ID)
		}},
	}
	for _, step := range steps {
		w := httptest.NewRecorder()
		step.run(w)
		if w.Code >= 300 {
			t.Fatalf("%s: %d %s", step.name, w.Code, w.Body.String())
		}
	}
	if missed := store.missedCalls(); len(missed) > 0 {
		t.Fatalf("store calls not made on the request's context: %v", missed)
	}
}

// TestAbandonedRequestIsNotA500 checks that a request the client abandoned
// is neither answered with a 500 nor logged as a failure.
func TestAbandonedRequestIsNotA500(t *testing.T) {
	s, store := newContextTestServer(t, nil)
	meme := store.createDirect(t, "abandoned")

	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	cases := map[string]func(w http.ResponseWriter){
		"get meme": func(w http.ResponseWriter) {
			s.handleMemeByID(w, requestFor(http.MethodGet, "/api/memes/"+meme.ID, nil, true))
		},
		"edit meme": func(w http.ResponseWriter) {
			s.handleMemeByID(w, requestFor(http.MethodPatch, "/api/memes/"+meme.ID, strings.NewReader(`{"notes":"x"}`), true))
		},
		"reel step": func(w http.ResponseWriter) {
			s.handleRandomMeme(w, requestFor(http.MethodGet, "/api/memes/random?direction=next", nil, true))
		},
		"share": func(w http.ResponseWriter) {
			s.createMemeShare(w, requestFor(http.MethodPost, "/api/memes/"+meme.ID+"/share", nil, true), meme.ID)
		},
	}
	for name, run := range cases {
		w := httptest.NewRecorder()
		run(w)
		if w.Code == http.StatusInternalServerError {
			t.Errorf("%s: abandoned request answered 500: %s", name, w.Body.String())
		}
	}
	if logs.Len() > 0 {
		t.Errorf("abandoned requests were logged:\n%s", logs.String())
	}
}

// TestUploadFinishesDespiteCancelledRequest checks that an upload received in
// full is saved and queued for tag suggestions even though its request has
// already been cancelled.
func TestUploadFinishesDespiteCancelledRequest(t *testing.T) {
	suggester := tagsuggest.New(tagsuggest.Config{OllamaURL: "http://127.0.0.1:0", Model: "stub"})
	s, store := newContextTestServer(t, suggester)

	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", "uploaded.png")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("\x89PNG\r\n\x1a\n uploaded while leaving"))
	_ = form.Close()

	r := requestFor(http.MethodPost, "/api/memes", &body, true)
	r.Header.Set("Content-Type", form.FormDataContentType())
	w := httptest.NewRecorder()
	s.createMeme(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("upload with a cancelled request: %d %s", w.Code, w.Body.String())
	}
	if memes := store.List(context.Background(), "", "uploaded", false, ""); len(memes) != 1 {
		t.Fatalf("stored %d memes, want 1", len(memes))
	}
	if queued := s.managers.TagSuggestionQueueStatus(context.Background(), 0, 10).QueueLength; queued != 1 {
		t.Fatalf("tag suggestion queue holds %d memes after the upload, want 1", queued)
	}
}

// TestReloadAfterRestoreRunsDespiteCancelledContext checks that the
// in-memory state is brought in line with a restore that has already
// replaced the database, even if the request that ran it is gone.
func TestReloadAfterRestoreRunsDespiteCancelledContext(t *testing.T) {
	s, store := newContextTestServer(t, nil)
	s.auth = testAuthService()
	store.createDirect(t, "before")
	if got := s.managers.AdminDashboard(requestScoped(context.Background())).Counts.Total; got != 1 {
		t.Fatalf("dashboard before the restore counted %d memes, want 1", got)
	}
	store.createDirect(t, "restored") // what the restore brought in

	ctx, cancel := context.WithCancel(requestScoped(context.Background()))
	cancel()
	s.reloadAfterRestore(ctx, "test restore")

	if got := s.managers.AdminDashboard(requestScoped(context.Background())).Counts.Total; got != 2 {
		t.Fatalf("dashboard after the restore counted %d memes, want 2 (the reload did not run)", got)
	}
}

// TestStorageReportIsNotCachedFromCancelledRequest checks that a storage
// report built for a request that went away part-way is not served later.
func TestStorageReportIsNotCachedFromCancelledRequest(t *testing.T) {
	s, store := newContextTestServer(t, nil)
	s.config.DataDir = t.TempDir()
	meme := store.createDirect(t, "sized")

	ctx, cancel := context.WithCancel(requestScoped(context.Background()))
	cancel()
	if _, err := s.cachedStorageReport(ctx, false); err != nil {
		t.Fatalf("storage report with a cancelled context: %v", err)
	}
	report, err := s.cachedStorageReport(requestScoped(context.Background()), false)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range report.Largest {
		if f.ID == meme.ID {
			return
		}
	}
	t.Fatalf("storage report after a cancelled one lists %v, want the meme's size (the partial report was cached)", report.Largest)
}

// deleteFilesStore is just enough of the audiobook store to delete one book,
// failing the delete if its context has been cancelled, as Postgres would.
type deleteFilesStore struct {
	audiobooks.Store
	record audiobooks.Record
}

func (s *deleteFilesStore) GetAudiobook(_ context.Context, id string) (audiobooks.Record, error) {
	if id != s.record.ID {
		return audiobooks.Record{}, audiobooks.ErrNotFound
	}
	return s.record, nil
}

func (s *deleteFilesStore) DeleteAudiobook(ctx context.Context, id string) (audiobooks.Record, error) {
	if err := ctx.Err(); err != nil {
		return audiobooks.Record{}, err
	}
	return s.GetAudiobook(ctx, id)
}

// TestAudiobookDeleteFinishesDespiteCancelledRequest checks that deleting an
// audiobook, which deletes the row and then the files, runs to the end even
// though the request has already been cancelled.
func TestAudiobookDeleteFinishesDespiteCancelledRequest(t *testing.T) {
	dir := t.TempDir()
	book := filepath.Join(dir, "book.mp3")
	cover := filepath.Join(dir, "book.jpg")
	for _, path := range []string{book, cover} {
		if err := os.WriteFile(path, []byte("bytes"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{audiobooks: &deleteFilesStore{record: audiobooks.Record{ID: "book-1", StoragePath: book, CoverPath: cover}}}

	w := httptest.NewRecorder()
	s.deleteAudiobook(w, requestFor(http.MethodDelete, "/api/audiobooks/book-1", nil, true), "book-1")
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete with a cancelled request: %d %s", w.Code, w.Body.String())
	}
	for _, path := range []string{book, cover} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s still on disk after the delete (stat err %v)", filepath.Base(path), err)
		}
	}
}
