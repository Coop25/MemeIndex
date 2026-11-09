package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"memeindex/internal/accessor"
	"memeindex/internal/manager"
)

// writeStaticFixture puts the test in a temp working directory holding a
// static/ folder with the given files, the layout the asset handlers read.
func writeStaticFixture(t *testing.T, files map[string]string) {
	t.Helper()
	t.Chdir(t.TempDir())
	if err := os.Mkdir("static", 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join("static", name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestIndexFingerprintsAdminPagesScript(t *testing.T) {
	writeStaticFixture(t, map[string]string{
		"index.html":     `<link href="/static/styles.css"><script src="/static/app.js"></script><script src="/static/admin-pages.js"></script>`,
		"app.js":         "app",
		"styles.css":     "css",
		"admin-pages.js": "admin v1",
	})
	pattern := regexp.MustCompile(`src="(/static/admin-pages\.js[^"]*)"`)
	render := func() string {
		t.Helper()
		recorder := httptest.NewRecorder()
		(&Server{}).handleIndex(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
		match := pattern.FindStringSubmatch(recorder.Body.String())
		if match == nil {
			t.Fatalf("admin-pages.js script tag missing: %s", recorder.Body.String())
		}
		return match[1]
	}

	first := render()
	if !strings.Contains(first, "h=") || !strings.Contains(first, "v=") {
		t.Fatalf("admin-pages.js URL is not fingerprinted: %q", first)
	}
	if err := os.WriteFile(filepath.Join("static", "admin-pages.js"), []byte("admin v2 with more bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if second := render(); second == first {
		t.Fatalf("admin-pages.js URL did not change after the file changed: %q", second)
	}
}

func TestUnfingerprintedStaticAssetsRevalidate(t *testing.T) {
	writeStaticFixture(t, map[string]string{"worker.js": "worker"})
	handler := staticAssetHandler("static")

	plain := httptest.NewRecorder()
	handler.ServeHTTP(plain, httptest.NewRequest(http.MethodGet, "/static/worker.js", nil))
	if got := plain.Header().Get("Cache-Control"); got != "private, no-cache" {
		t.Fatalf("unfingerprinted asset Cache-Control = %q, want revalidation", got)
	}

	fingerprinted := httptest.NewRecorder()
	handler.ServeHTTP(fingerprinted, httptest.NewRequest(http.MethodGet, "/static/worker.js?v=1&h=abc", nil))
	if got := fingerprinted.Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Fatalf("fingerprinted asset Cache-Control = %q, want immutable", got)
	}
}

func TestSharedNotesNeverSplitsACharacter(t *testing.T) {
	// 1999 ASCII bytes followed by a 4-byte emoji straddles the 2000-byte cap.
	note := sharedNotes("", strings.Repeat("a", 1999)+"😀 trailing text")
	if !utf8.ValidString(note) {
		t.Fatalf("capped note is not valid UTF-8 (ends %q)", note[len(note)-4:])
	}
	if len(note) > 2000 {
		t.Fatalf("capped note is %d bytes, want at most 2000", len(note))
	}
}

// failingCreateStore fails every Create whose filename is in failNames.
type failingCreateStore struct {
	*accessor.MemeStore
	failNames map[string]bool
}

func (s failingCreateStore) Create(input accessor.CreateInput) (accessor.Meme, error) {
	if s.failNames[input.Filename] {
		return accessor.Meme{}, errors.New("disk full")
	}
	return s.MemeStore.Create(input)
}

func multiFileUploadRequest(t *testing.T, files map[string]string, order []string) *http.Request {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	for _, name := range order {
		part, err := writer.CreateFormFile("file", name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write([]byte(files[name])); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/memes", body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
}

func TestCreateMemeReportsSavedFilesWhenALaterFileFails(t *testing.T) {
	base, err := accessor.NewMemeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store := failingCreateStore{MemeStore: base, failNames: map[string]bool{"bad.png": true}}
	server := &Server{managers: manager.NewMemeManager(store)}

	request := multiFileUploadRequest(t,
		map[string]string{"good.png": "first file", "bad.png": "second file", "also-good.png": "third file"},
		[]string{"good.png", "bad.png", "also-good.png"})
	recorder := httptest.NewRecorder()
	server.createMeme(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Created int `json:"created"`
		Failed  []struct {
			Filename string `json:"filename"`
		} `json:"failed"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Created != 2 {
		t.Fatalf("created = %d, want 2 (the files around the failure)", payload.Created)
	}
	if len(payload.Failed) != 1 || payload.Failed[0].Filename != "bad.png" {
		t.Fatalf("failed = %+v, want only bad.png", payload.Failed)
	}
}

func TestCreateMemeFailsWhenNoFileCouldBeSaved(t *testing.T) {
	base, err := accessor.NewMemeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store := failingCreateStore{MemeStore: base, failNames: map[string]bool{"bad.png": true}}
	server := &Server{managers: manager.NewMemeManager(store)}

	recorder := httptest.NewRecorder()
	server.createMeme(recorder, multiFileUploadRequest(t, map[string]string{"bad.png": "only file"}, []string{"bad.png"}))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 when nothing was saved", recorder.Code)
	}
}

func backupTableForTest(t *testing.T, name string) backupTable {
	t.Helper()
	for _, table := range portableBackupTables {
		if table.name == name {
			return table
		}
	}
	t.Fatalf("backup table %q not found", name)
	return backupTable{}
}

// A restore that resets session_version lets a token the user already logged
// out of validate again, and one that drops the share columns kills every
// active share link.
func TestBackupCarriesSessionVersionsAndShareLinks(t *testing.T) {
	want := map[string][]string{
		"app_users":               {"session_version"},
		"app_user_readd_required": {"session_version"},
		"memes":                   {"share_generation", "share_expires_at", "shared_at", "shared_by_user_id"},
	}
	for name, columns := range want {
		table := backupTableForTest(t, name)
		current := strings.Split(table.columns, ", ")
		for _, column := range columns {
			if !slicesContains(current, column) {
				t.Errorf("%s backup columns are missing %s", name, column)
			}
			for _, legacy := range []int{3, 4} {
				if slicesContains(strings.Split(table.columnsForFormat(legacy), ", "), column) {
					t.Errorf("%s format-%d column list must not include %s: archives of that format never wrote it", name, legacy, column)
				}
			}
		}
	}
}

func slicesContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestValidateRestoredMemesUsesTheArchiveFormatsColumns(t *testing.T) {
	staging := t.TempDir()
	for _, dir := range []string{"database", "uploads"} {
		if err := os.MkdirAll(filepath.Join(staging, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(staging, "uploads", "abc.webp"), []byte("meme"), 0o600); err != nil {
		t.Fatal(err)
	}
	columns := strings.Split(backupTableForTest(t, "memes").columnsForFormat(4), ", ")
	values := make([]string, len(columns))
	values[0], values[2], values[3] = "abc", "abc.webp", "/uploads/abc.webp"
	csv := strings.Join(columns, ",") + "\n" + strings.Join(values, ",") + "\n"
	if err := os.WriteFile(filepath.Join(staging, "database", "memes.csv"), []byte(csv), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := validateRestoredMemes(staging, 4); err != nil {
		t.Fatalf("format-4 memes.csv rejected for a format-4 archive: %v", err)
	}
	if err := validateRestoredMemes(staging, portableBackupFormatVersion); err == nil {
		t.Fatal("format-4 memes.csv accepted for a current-format archive")
	}
}

func newPersistedRetryQueue(t *testing.T, path string, interval time.Duration) *linkRetryQueue {
	t.Helper()
	q := newLinkRetryQueue(interval, 1, func(context.Context, LinkRetryJob) error { return errors.New("still down") })
	if err := q.persistTo(path); err != nil {
		t.Fatalf("persistTo: %v", err)
	}
	return q
}

func TestLinkRetryQueueSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "link_retries.json")
	first := newPersistedRetryQueue(t, path, time.Hour)
	queued := first.Enqueue("https://example.com/a.mp4", []string{"cat"}, "note", accessor.AuditActor{UserID: "u1"})

	restarted := newPersistedRetryQueue(t, path, time.Hour).Snapshot()
	if len(restarted.Queued) != 1 {
		t.Fatalf("queued jobs after restart = %d, want 1", len(restarted.Queued))
	}
	got := restarted.Queued[0]
	if got.ID != queued.ID || got.SourceURL != queued.SourceURL || got.Actor.UserID != "u1" || got.Notes != "note" || len(got.Tags) != 1 {
		t.Fatalf("job restored wrong: %+v", got)
	}
}

func TestLinkRetryQueueKeepsRejectedAndInFlightJobsAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "link_retries.json")
	first := newPersistedRetryQueue(t, path, time.Nanosecond)
	rejected := first.Enqueue("https://example.com/rejected.mp4", nil, "", accessor.AuditActor{})
	time.Sleep(2 * time.Millisecond)
	job, ok := first.dequeueReady()
	if !ok {
		t.Fatal("expected a ready job")
	}
	first.finish(job, errors.New("gone")) // maxAttempts is 1, so this rejects it
	inFlight := first.Enqueue("https://example.com/inflight.mp4", nil, "", accessor.AuditActor{})
	time.Sleep(2 * time.Millisecond)
	if _, ok := first.dequeueReady(); !ok {
		t.Fatal("expected the second job to be ready")
	}
	// The process dies here, mid-download: finish is never called.

	restarted := newPersistedRetryQueue(t, path, time.Hour).Snapshot()
	if len(restarted.Rejected) != 1 || restarted.Rejected[0].ID != rejected.ID {
		t.Fatalf("rejected jobs after restart = %+v", restarted.Rejected)
	}
	if len(restarted.Queued) != 1 || restarted.Queued[0].ID != inFlight.ID {
		t.Fatalf("an in-flight job must be queued again after a restart, got %+v", restarted.Queued)
	}
	if restarted.ProcessingID != "" {
		t.Fatalf("nothing is processing right after a restart, got %q", restarted.ProcessingID)
	}
}

// Without a database there is no audiobook store, so the session payload has
// to say so: the client otherwise shows an Audiobooks section whose every
// request answers 501.
func TestAuthSessionReportsAudiobooksUnavailableWithoutAStore(t *testing.T) {
	server, _ := newShareTargetTestServer(t)
	recorder := httptest.NewRecorder()
	server.handleAuthSession(recorder, httptest.NewRequest(http.MethodGet, "/api/auth/session", nil))

	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if available, ok := payload["audiobooks_available"].(bool); !ok || available {
		t.Fatalf("audiobooks_available = %v, want false", payload["audiobooks_available"])
	}
}

// Meme files and thumbnails may be kept by the viewer's own browser as long as
// it re-checks with the server (and so re-runs the session check) every time.
func TestProtectedAssetsRevalidateInsteadOfRedownloading(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "abc.png"), []byte("png bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	server, _ := newShareTargetTestServer(t)
	handler := server.withProtectedAssetAuth(http.StripPrefix("/uploads/", http.FileServer(http.Dir(dir))))

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/uploads/abc.png", nil))
	if first.Code != http.StatusOK {
		t.Fatalf("status = %d", first.Code)
	}
	if got := first.Header().Get("Cache-Control"); got != "private, no-cache" {
		t.Fatalf("Cache-Control = %q, want a revalidated private cache", got)
	}
	if got := first.Header().Get("Cloudflare-CDN-Cache-Control"); !strings.Contains(got, "no-store") {
		t.Fatalf("a shared cache must still never store the file, got %q", got)
	}
	lastModified := first.Header().Get("Last-Modified")
	if lastModified == "" {
		t.Fatal("no Last-Modified validator, so the browser cannot revalidate")
	}

	again := httptest.NewRequest(http.MethodGet, "/uploads/abc.png", nil)
	again.Header.Set("If-Modified-Since", lastModified)
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, again)
	if second.Code != http.StatusNotModified {
		t.Fatalf("revalidation status = %d, want 304", second.Code)
	}
	if second.Body.Len() != 0 {
		t.Fatalf("revalidation re-sent %d bytes", second.Body.Len())
	}
}

// The home screen polls for new memes every few seconds; the probe answers
// that question without building the whole dashboard.
func TestDashboardProbeReturnsOnlyTheNewestMemeID(t *testing.T) {
	server, store := newShareTargetTestServer(t)
	probe := func() map[string]any {
		t.Helper()
		recorder := httptest.NewRecorder()
		server.handleDashboard(recorder, httptest.NewRequest(http.MethodGet, "/api/dashboard?probe=newest", nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d", recorder.Code)
		}
		var payload map[string]any
		if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		return payload
	}

	if got := probe(); got["newest_id"] != "" {
		t.Fatalf("empty vault newest_id = %v, want empty", got["newest_id"])
	}
	if _, err := store.Create(accessor.CreateInput{File: strings.NewReader("older"), Filename: "older.png"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	newest, err := store.Create(accessor.CreateInput{File: strings.NewReader("newer"), Filename: "newer.png"})
	if err != nil {
		t.Fatal(err)
	}

	got := probe()
	if got["newest_id"] != newest.ID {
		t.Fatalf("newest_id = %v, want %s", got["newest_id"], newest.ID)
	}
	if len(got) != 1 {
		t.Fatalf("probe returned more than the newest id: %v", got)
	}
}
