package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"memeindex/internal/audiobooks"
)

func testAuthService() *authService {
	return newAuthService(DiscordAuthConfig{
		ClientID: "client", ClientSecret: "secret", RedirectURL: "http://localhost/auth/callback",
		SessionSecret: "session-secret", SessionDuration: time.Hour,
	}, nil)
}

func TestAuthInvalidateAllDropsEveryCachedSession(t *testing.T) {
	a := testAuthService()
	expires := time.Now().Add(time.Hour)
	a.storeSession("token-a", authSession{UserID: "alice", ExpiresAt: expires})
	a.storeSession("token-b", authSession{UserID: "bob", ExpiresAt: expires})
	a.sessionMu.Lock()
	a.lastProfileWrite["alice"] = time.Now()
	a.sessionMu.Unlock()

	a.invalidateAll()
	for _, key := range []string{"token-a", "token-b"} {
		if _, ok := a.cachedSession(key); ok {
			t.Fatalf("session %s still cached after invalidateAll", key)
		}
	}
	a.sessionMu.Lock()
	throttled := len(a.lastProfileWrite)
	a.sessionMu.Unlock()
	if throttled != 0 {
		t.Fatal("profile write throttle survived invalidateAll")
	}
	(*authService)(nil).invalidateAll() // auth disabled: a no-op
}

// A successful restore replaces app_users and the media directories, so the
// caches built from the old state must go: sessions, the admin dashboard's
// audiobook aggregates, the storage report and the orphan-scan allow-list.
func TestBackupRestoreResetsServerCaches(t *testing.T) {
	s, _, _ := newShareTestServer(t)
	s.backup = &portableBackup{dataDir: t.TempDir(), job: backupJobStatus{State: "idle"}}
	s.auth = testAuthService()
	s.auth.storeSession("token", authSession{UserID: "gone-after-restore", ExpiresAt: time.Now().Add(time.Hour)})
	s.audiobookAdminStats.valid = true
	s.audiobookActivity.valid = true
	s.storage.reportValid = true
	s.storage.setScan([]orphanFile{{Path: "uploads/stale.png"}})

	r := httptest.NewRequest(http.MethodPost, "https://example.com/api/admin/backup/import", strings.NewReader(""))
	w := httptest.NewRecorder()
	s.runBackupRestore(w, r, func(context.Context) error { return nil })
	if w.Code != http.StatusOK {
		t.Fatalf("restore: %d %s", w.Code, w.Body.String())
	}

	if _, ok := s.auth.cachedSession("token"); ok {
		t.Fatal("a session cached before the restore is still served")
	}
	if s.audiobookAdminStats.valid || s.audiobookActivity.valid {
		t.Fatal("audiobook admin aggregates survived the restore")
	}
	if s.storage.reportValid {
		t.Fatal("storage report survived the restore")
	}
	if _, scanned := s.storage.scanAllowed("uploads/stale.png"); scanned {
		t.Fatal("orphan scan from before the restore still allows deletes")
	}
}

// A failed restore must not echo database text or this server's file paths,
// but must still tell the admin what is wrong with the backup itself.
func TestBackupRestoreErrorsAreClientSafe(t *testing.T) {
	s, _, _ := newShareTestServer(t)
	s.backup = &portableBackup{dataDir: t.TempDir(), job: backupJobStatus{State: "idle"}}

	restore := func(err error) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "https://example.com/api/admin/backup/import", strings.NewReader(""))
		w := httptest.NewRecorder()
		s.runBackupRestore(w, r, func(context.Context) error { return err })
		return w
	}

	internal := restore(fmt.Errorf("restore table memes: %w", errors.New(`ERROR: permission denied for table memes (SQLSTATE 42501) at /srv/memeindex/data`)))
	if internal.Code != http.StatusInternalServerError {
		t.Fatalf("internal failure: got %d, want 500", internal.Code)
	}
	if body := internal.Body.String(); strings.Contains(body, "SQLSTATE") || strings.Contains(body, "/srv/memeindex") {
		t.Fatalf("internal failure leaked detail to the client: %q", body)
	}

	for _, err := range []error{
		userError("backup is missing database/memes.csv"),
		fmt.Errorf("validate: %w", userError("backup is missing database/memes.csv")),
	} {
		w := restore(err)
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "backup is missing database/memes.csv") {
			t.Fatalf("validation failure: got %d %q, want 400 with the reason", w.Code, w.Body.String())
		}
	}

	wrapped := restore(userErrorWrap("the backup archive is truncated or damaged", errors.New("read /srv/memeindex/data/.memeindex-import-1: unexpected EOF")))
	if body := wrapped.Body.String(); !strings.Contains(body, "truncated or damaged") || strings.Contains(body, "/srv/memeindex") {
		t.Fatalf("wrapped validation failure: %q", body)
	}
}

// The validation failures admins need to act on keep their wording through
// the restore path.
func TestRestoreValidationErrorsReachTheAdmin(t *testing.T) {
	dir := t.TempDir()
	b := &portableBackup{dataDir: dir}
	archive := makeTestArchive(t, map[string]string{"manifest.json": `{"format_version":1}`})
	err := b.restoreArchive(context.Background(), strings.NewReader(string(archive)), defaultMaxExtractBytes, nil)
	if got := clientSafeError(err, "generic"); !strings.Contains(got, "unsupported backup format version 1") {
		t.Fatalf("format version error = %q (from %v)", got, err)
	}

	archive = makeTestArchive(t, map[string]string{"manifest.json": fmt.Sprintf(`{"format_version":%d}`, portableBackupFormatVersion)})
	err = b.restoreArchive(context.Background(), strings.NewReader(string(archive)), defaultMaxExtractBytes, nil)
	if got := clientSafeError(err, "generic"); got != "backup is missing database/memes.csv" {
		t.Fatalf("missing table error = %q (from %v)", got, err)
	}

	err = b.restoreArchive(context.Background(), strings.NewReader("not a gzip stream"), defaultMaxExtractBytes, nil)
	if got := clientSafeError(err, "generic"); got != "file is not a valid MemeIndex backup" {
		t.Fatalf("garbage archive error = %q (from %v)", got, err)
	}

	err = requireStagedAudiobookFile(filepath.Join(dir, "audiobooks"), filepath.Join(dir, "staging"), "/elsewhere/audiobooks/book.mp3")
	if got := clientSafeError(err, "generic"); !strings.Contains(got, "different MEMEINDEX_DATA_DIR") {
		t.Fatalf("data-dir mismatch error = %q", got)
	}
}

// A migration whose restore fails inside this server reports a generic
// reason on the admin page; the detail, with its file paths, goes to the log.
func TestMigrationRestoreFailureIsClientSafe(t *testing.T) {
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("archive"))
	}))
	defer source.Close()

	missing := filepath.Join(t.TempDir(), "no-such-data-dir")
	s := &Server{backup: &portableBackup{
		dataDir:    missing,
		exportFunc: func(context.Context) (string, error) { return "", errors.New("no database in this test") },
	}}
	base, _ := url.Parse(source.URL)
	err := s.runMigrationImport(context.Background(), base, "mig_x")
	if err == nil {
		t.Fatal("expected the restore to fail")
	}
	shown := clientSafeError(err, err.Error())
	if strings.Contains(shown, missing) || !strings.Contains(shown, "restore failed on this server") {
		t.Fatalf("migration status error = %q", shown)
	}
	if !strings.Contains(err.Error(), "no-such-data-dir") {
		t.Fatalf("logged error lost its detail: %v", err)
	}
}

func TestMigrationInfoHidesDatabaseErrors(t *testing.T) {
	s := &Server{backup: &portableBackup{databaseURL: "postgres://user:hunter2@127.0.0.1:1/db?connect_timeout=1", dataDir: t.TempDir()}}
	w := httptest.NewRecorder()
	s.handleMigrationInfo(w, httptest.NewRequest(http.MethodGet, "/api/migration/info", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("got %d, want 500", w.Code)
	}
	if body := w.Body.String(); strings.Contains(body, "127.0.0.1") || strings.Contains(body, "hunter2") || strings.Contains(body, "dial") {
		t.Fatalf("migration info leaked connection detail: %q", body)
	}
}

// failingCompleteStore fails CompleteUpload with an internal error.
type failingCompleteStore struct{ audiobooks.Store }

func (failingCompleteStore) CompleteUpload(context.Context, string, string, float64) (audiobooks.Record, error) {
	return audiobooks.Record{}, errors.New("move completed audiobook: rename /srv/memeindex/data/audiobooks/tmp/x.part: permission denied")
}

func TestCompleteAudiobookUploadHidesInternalErrors(t *testing.T) {
	s := &Server{audiobooks: failingCompleteStore{}}
	r := httptest.NewRequest(http.MethodPost, "https://example.com/api/audiobooks/uploads/x/complete", strings.NewReader(""))
	w := httptest.NewRecorder()
	s.handleAudiobookUploadByID(w, r)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("got %d, want 500", w.Code)
	}
	if strings.Contains(w.Body.String(), "/srv/memeindex") {
		t.Fatalf("complete leaked a server path: %q", w.Body.String())
	}

	s.audiobooks = &uploadLockStore{completeErr: audiobooks.ErrDuplicateTitle}
	w = httptest.NewRecorder()
	s.handleAudiobookUploadByID(w, httptest.NewRequest(http.MethodPost, "https://example.com/api/audiobooks/uploads/known/complete", strings.NewReader("")))
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), audiobooks.ErrDuplicateTitle.Error()) {
		t.Fatalf("duplicate: got %d %q, want 409 with the reason", w.Code, w.Body.String())
	}
}
