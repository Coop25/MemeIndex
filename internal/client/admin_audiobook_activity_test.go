package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"memeindex/internal/accessor"
	"memeindex/internal/manager"
)

func insertActivityBook(t *testing.T, store *postgresAudiobookStore, title, coverPath, chapters, seriesID string) string {
	t.Helper()
	id := uuid.Must(uuid.NewV6()).String()
	var series any
	if seriesID != "" {
		series = seriesID
	}
	_, err := store.pool.Exec(context.Background(), `
		INSERT INTO audiobooks (id, title, filename, size_bytes, duration_seconds, storage_path, cover_path, uploaded_by_user_id, chapters, series_id)
		VALUES ($1, $2, 'book.m4b', 1, 60, '/tmp/book.m4b', $3, 'u-admin', $4::jsonb, $5)
	`, id, title, coverPath, chapters, series)
	if err != nil {
		t.Fatalf("insert audiobook: %v", err)
	}
	return id
}

func insertListening(t *testing.T, store *postgresAudiobookStore, user, book string, date time.Time, seconds float64) {
	t.Helper()
	if _, err := store.pool.Exec(context.Background(), `
		INSERT INTO audiobook_listening_stats (user_id, audiobook_id, listen_date, seconds_listened)
		VALUES ($1, $2, $3, $4)
	`, user, book, date.Format("2006-01-02"), seconds); err != nil {
		t.Fatalf("insert listening: %v", err)
	}
}

func TestAdminListening(t *testing.T) {
	store, _ := newIsolatedAudiobookStore(t)
	now := time.Now().UTC()
	day := 24 * time.Hour
	b1 := insertActivityBook(t, store, "Book One", "", "[]", "")
	b2 := insertActivityBook(t, store, "Book Two", "", "[]", "")
	insertListening(t, store, "u1", b1, now, 600)
	insertListening(t, store, "u1", b1, now.Add(-3*day), 300)
	insertListening(t, store, "u2", b2, now, 100)
	insertListening(t, store, "u2", b2, now.Add(-45*day), 999)

	daily, books, listeners, err := store.AdminListening(context.Background(), now)
	if err != nil {
		t.Fatalf("AdminListening: %v", err)
	}
	if len(daily) != 30 {
		t.Fatalf("len(daily) = %d, want 30", len(daily))
	}
	if daily[29].Date != now.Format("2006-01-02") || daily[29].Seconds != 700 {
		t.Errorf("daily[29] = %+v, want today with 700", daily[29])
	}
	if daily[26].Seconds != 300 {
		t.Errorf("daily[26] = %+v, want 300", daily[26])
	}
	if len(books) != 2 || books[0].ID != b1 || books[0].Seconds != 900 || books[0].Title != "Book One" || books[1].ID != b2 || books[1].Seconds != 100 {
		t.Errorf("books = %+v", books)
	}
	if len(listeners) != 2 || listeners[0].UserID != "u1" || listeners[0].Seconds != 900 || listeners[1].UserID != "u2" || listeners[1].Seconds != 100 {
		t.Errorf("listeners = %+v", listeners)
	}
}

func TestAdminHealthCounts(t *testing.T) {
	store, _ := newIsolatedAudiobookStore(t)
	ctx := context.Background()
	if _, err := store.pool.Exec(ctx, `INSERT INTO audiobook_series (id, name) VALUES ('s1', 'Series')`); err != nil {
		t.Fatalf("insert series: %v", err)
	}
	one := `[{"title":"Ch 1","start_seconds":0,"end_seconds":10}]`
	insertActivityBook(t, store, "full", "/c/a.jpg", one, "s1")
	insertActivityBook(t, store, "no cover", "", one, "s1")
	insertActivityBook(t, store, "no chapters", "/c/b.jpg", "[]", "s1")
	insertActivityBook(t, store, "no series", "/c/c.jpg", one, "")
	insertActivityBook(t, store, "nothing", "", "[]", "")

	got, err := store.AdminHealthCounts(ctx)
	if err != nil {
		t.Fatalf("AdminHealthCounts: %v", err)
	}
	if got.MissingCover != 2 || got.MissingChapters != 2 || got.MissingSeries != 2 {
		t.Errorf("health = %+v, want 2/2/2", got)
	}
	if got.AwaitingConversion != 0 || got.FailedConversion != 0 {
		t.Errorf("conversion counts = %+v, want 0", got)
	}
}

func TestFailedRemuxAudiobookIDs(t *testing.T) {
	store, dir := newIsolatedAudiobookStore(t)
	ctx := context.Background()
	failedPath, okPath := dir+"/failed.mp3", dir+"/ok.mp3"
	for _, p := range []string{failedPath, okPath} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	failedID, okID := uuid.Must(uuid.NewV6()).String(), uuid.Must(uuid.NewV6()).String()
	for id, p := range map[string]string{failedID: failedPath, okID: okPath} {
		if _, err := store.pool.Exec(ctx, `
			INSERT INTO audiobooks (id, title, filename, size_bytes, duration_seconds, storage_path, mime_type, uploaded_by_user_id)
			VALUES ($1, 'T', 'a.mp3', 1, 1, $2, 'audio/mpeg', 'u')`, id, p); err != nil {
			t.Fatal(err)
		}
	}
	recordAudiobookRemuxFailure(failedPath, os.ErrInvalid)

	s := &Server{audiobooks: store}
	ids, err := s.failedRemuxAudiobookIDs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != failedID {
		t.Errorf("ids = %v, want [%s]", ids, failedID)
	}
	health, err := store.AdminHealthCounts(ctx)
	if err != nil || health.AwaitingConversion != 2 {
		t.Errorf("health = %+v, err %v", health, err)
	}
}

// pendingDeleteStore reports a fixed number of pending deletes while the
// file-backed MemeStore stays the real store underneath.
type pendingDeleteStore struct {
	*accessor.MemeStore
	accessor.AuditLogStore
	pending int
}

func (p pendingDeleteStore) ListPendingDeletes(offset, limit int) (accessor.PagedPendingDeletes, error) {
	return accessor.PagedPendingDeletes{Total: p.pending}, nil
}

func (p pendingDeleteStore) ListAuditFeed(offset, limit int, actorUserID string) (accessor.PagedAuditFeed, error) {
	return accessor.PagedAuditFeed{}, nil
}

func TestAdminDashboardWorkQueues(t *testing.T) {
	s, store, _ := newShareTestServer(t)
	s.config.DataDir = t.TempDir()
	s.managers = manager.NewMemeManager(pendingDeleteStore{MemeStore: store, pending: 1})
	s.linkRetries = newLinkRetryQueue(time.Hour, 3, func(context.Context, LinkRetryJob) error { return nil })
	s.linkRetries.Enqueue("https://example.com/a.png", nil, "", accessor.AuditActor{})
	s.auth = newAuthService(DiscordAuthConfig{ClientID: "c", ClientSecret: "s", RedirectURL: "https://example.com/auth/callback", SessionSecret: "test"}, &securityUsers{permissions: authPermissions{CanView: true, CanManageUsers: true}})
	token, err := s.auth.issueSessionToken(authSession{UserID: "admin", SessionVersion: 1, ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "https://example.com/api/admin/dashboard", nil)
	w := httptest.NewRecorder()
	s.auth.setSessionCookie(w, r, token, time.Now().Add(time.Hour))
	r.AddCookie(w.Result().Cookies()[0])
	w = httptest.NewRecorder()
	s.Routes().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		WorkQueues adminWorkQueues `json:"work_queues"`
		Activity   json.RawMessage `json:"audiobook_activity"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.WorkQueues.DeleteQueue != 1 || body.WorkQueues.LinkRetries != 1 || body.WorkQueues.RejectedLinks != 0 || body.WorkQueues.ConversionQueue != 0 {
		t.Errorf("work_queues = %+v", body.WorkQueues)
	}
	if body.Activity != nil {
		t.Errorf("audiobook_activity present without postgres: %s", body.Activity)
	}
}

// partialFailureStore fails health counts and conversions but serves listening.
type partialFailureStore struct {
	audiobookStore
	failListening bool
}

func (p partialFailureStore) AdminListening(context.Context, time.Time) ([]adminDaySeconds, []adminTopBook, []adminTopListener, error) {
	if p.failListening {
		return nil, nil, nil, os.ErrInvalid
	}
	return []adminDaySeconds{{Date: "2026-11-05", Seconds: 42}}, nil, nil, nil
}

func (p partialFailureStore) AdminHealthCounts(context.Context) (adminAudiobookHealth, error) {
	return adminAudiobookHealth{}, os.ErrInvalid
}

func (p partialFailureStore) ListStoredMP3s(context.Context) ([]audiobookStoredMP3, error) {
	return nil, os.ErrInvalid
}

func TestCachedAudiobookActivityPartialFailure(t *testing.T) {
	s := &Server{audiobooks: partialFailureStore{}}
	got, err := s.cachedAudiobookActivity(context.Background())
	if err != nil || got == nil {
		t.Fatalf("got %v, err %v; want partial activity", got, err)
	}
	if len(got.DailySeconds) != 1 || got.DailySeconds[0].Seconds != 42 {
		t.Errorf("daily = %+v, want listening kept", got.DailySeconds)
	}
	if got.Health != (adminAudiobookHealth{}) {
		t.Errorf("health = %+v, want zeroed", got.Health)
	}
	if !got.Partial {
		t.Error("Partial = false, want true")
	}
	if again, _ := s.cachedAudiobookActivity(context.Background()); again == nil || !again.Partial {
		t.Errorf("cached result lost the partial flag: %+v", again)
	}

	s = &Server{audiobooks: partialFailureStore{failListening: true}}
	if got, err := s.cachedAudiobookActivity(context.Background()); err == nil {
		t.Errorf("all parts failed: got %v, want error", got)
	}
}
