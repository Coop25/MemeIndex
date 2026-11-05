package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
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
	"memeindex/internal/manager"
)

// extraRefsMemeStore lets a test add meme rows that reference arbitrary stored
// names, the way a meme row created between a scan and a delete would.
type extraRefsMemeStore struct {
	*accessor.MemeStore
	mu    sync.Mutex
	extra []accessor.StoredFile
}

func (s *extraRefsMemeStore) AllStoredFiles() ([]accessor.StoredFile, error) {
	files, err := s.MemeStore.AllStoredFiles()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return append(files, s.extra...), nil
}

func (s *extraRefsMemeStore) addRef(storedName string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.extra = append(s.extra, accessor.StoredFile{ID: "late-" + storedName, StoredName: storedName})
}

type storageTestServer struct {
	s       *Server
	dataDir string
	parent  string
	store   *extraRefsMemeStore
	token   string
}

// newStorageTestServer builds an admin-authenticated server whose data dir is
// a subdirectory of a temp dir, so the parent can hold files that must never
// be touched.
func newStorageTestServer(t *testing.T) *storageTestServer {
	t.Helper()
	parent := t.TempDir()
	dataDir := filepath.Join(parent, "data")
	memes, err := accessor.NewMemeStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	store := &extraRefsMemeStore{MemeStore: memes}
	s := &Server{config: Config{DataDir: dataDir}, managers: manager.NewMemeManager(store), shareSecret: []byte("test-share-secret")}
	s.auth = newAuthService(DiscordAuthConfig{ClientID: "c", ClientSecret: "s", RedirectURL: "https://example.com/auth/callback", SessionSecret: "test"}, &securityUsers{permissions: authPermissions{CanView: true, CanManageUsers: true}})
	token, err := s.auth.issueSessionToken(authSession{UserID: "admin", SessionVersion: 1, ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	return &storageTestServer{s: s, dataDir: dataDir, parent: parent, store: store, token: token}
}

func (ts *storageTestServer) do(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	r := httptest.NewRequest(method, "https://example.com"+path, reader)
	w := httptest.NewRecorder()
	ts.s.auth.setSessionCookie(w, r, ts.token, time.Now().Add(time.Hour))
	r.AddCookie(w.Result().Cookies()[0])
	w = httptest.NewRecorder()
	ts.s.Routes().ServeHTTP(w, r)
	return w
}

// writeOld writes a file whose mtime is past the orphan grace period.
func writeOld(t *testing.T, path string) {
	t.Helper()
	writeSized(t, path, 4)
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
}

type orphanDeleteResponse struct {
	Results []struct {
		Path    string `json:"path"`
		Deleted bool   `json:"deleted"`
		Error   string `json:"error"`
	} `json:"results"`
	BytesFreed int64 `json:"bytes_freed"`
}

func (ts *storageTestServer) scan(t *testing.T) []orphanFile {
	t.Helper()
	w := ts.do(t, http.MethodPost, "/api/admin/storage/orphans/scan", "")
	if w.Code != http.StatusOK {
		t.Fatalf("scan status = %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Orphans   []orphanFile  `json:"orphans"`
		Missing   []missingFile `json:"missing"`
		ScannedAt time.Time     `json:"scanned_at"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.ScannedAt.IsZero() || body.Missing == nil {
		t.Fatalf("bad scan response: %s", w.Body.String())
	}
	return body.Orphans
}

func (ts *storageTestServer) deletePaths(t *testing.T, paths ...string) orphanDeleteResponse {
	t.Helper()
	payload, _ := json.Marshal(map[string][]string{"paths": paths})
	w := ts.do(t, http.MethodPost, "/api/admin/storage/orphans/delete", string(payload))
	if w.Code != http.StatusOK {
		t.Fatalf("delete status = %d: %s", w.Code, w.Body.String())
	}
	var body orphanDeleteResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Results) != len(paths) {
		t.Fatalf("results = %+v, want %d entries", body.Results, len(paths))
	}
	return body
}

func TestStorageOrphanDeleteRefusesUnscannedAndEscaping(t *testing.T) {
	ts := newStorageTestServer(t)
	orphan := filepath.Join(ts.dataDir, "uploads", "b.png")
	writeOld(t, orphan)
	outside := filepath.Join(ts.parent, "outside.txt")
	writeOld(t, outside)

	orphans := ts.scan(t)
	if len(orphans) != 1 || orphans[0].Path != orphan {
		t.Fatalf("orphans = %+v, want only %s", orphans, orphan)
	}
	// a.png appears after the scan, so it is not in the allow-list.
	unscanned := filepath.Join(ts.dataDir, "uploads", "a.png")
	writeOld(t, unscanned)

	paths := []string{
		ts.dataDir + "/uploads/b.png",
		ts.dataDir + "/../outside.txt",
		ts.dataDir + "/uploads/a.png",
		"/etc/hostname",
	}
	got := ts.deletePaths(t, paths...)
	for i, res := range got.Results {
		if res.Path != paths[i] {
			t.Errorf("result %d path = %q, want %q", i, res.Path, paths[i])
		}
		if i == 0 {
			if !res.Deleted || res.Error != "" {
				t.Errorf("b.png not deleted: %+v", res)
			}
			continue
		}
		if res.Deleted || res.Error == "" {
			t.Errorf("%s: deleted=%v error=%q, want refused with an error", res.Path, res.Deleted, res.Error)
		}
	}
	if got.BytesFreed != 4 {
		t.Errorf("bytes_freed = %d, want 4", got.BytesFreed)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Errorf("b.png still exists: %v", err)
	}
	for _, p := range []string{outside, unscanned} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s was removed: %v", p, err)
		}
	}

	// A path deleted once is gone from the allow-list.
	again := ts.deletePaths(t, orphan)
	if again.Results[0].Deleted {
		t.Error("second delete of the same path reported deleted")
	}
}

func TestStorageOrphanDeleteRechecksReferences(t *testing.T) {
	ts := newStorageTestServer(t)
	orphan := filepath.Join(ts.dataDir, "uploads", "b.png")
	writeOld(t, orphan)
	if orphans := ts.scan(t); len(orphans) != 1 {
		t.Fatalf("orphans = %+v", orphans)
	}
	ts.store.addRef("b.png")
	got := ts.deletePaths(t, orphan)
	if got.Results[0].Deleted || got.Results[0].Error == "" {
		t.Errorf("referenced file deleted: %+v", got.Results[0])
	}
	if _, err := os.Stat(orphan); err != nil {
		t.Errorf("b.png removed: %v", err)
	}
}

func TestStorageOrphanDeleteRefusesWithoutScanAndDuplicates(t *testing.T) {
	ts := newStorageTestServer(t)
	orphan := filepath.Join(ts.dataDir, "uploads", "b.png")
	writeOld(t, orphan)
	got := ts.deletePaths(t, orphan)
	if got.Results[0].Deleted || got.Results[0].Error == "" {
		t.Errorf("deleted without a scan: %+v", got.Results[0])
	}
	ts.scan(t)
	got = ts.deletePaths(t, orphan, orphan, "uploads/b.png")
	if !got.Results[0].Deleted || got.Results[1].Deleted || got.Results[2].Deleted {
		t.Errorf("results = %+v, want only the first deleted", got.Results)
	}
}

func TestStorageOrphanDeleteRefusesSymlinkedDir(t *testing.T) {
	ts := newStorageTestServer(t)
	elsewhere := filepath.Join(ts.parent, "elsewhere")
	writeOld(t, filepath.Join(elsewhere, "x.jpg"))
	thumbs := filepath.Join(ts.dataDir, "thumbnails")
	if err := os.RemoveAll(thumbs); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, thumbs); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	target := filepath.Join(thumbs, "x.jpg")
	orphans := ts.scan(t)
	found := false
	for _, o := range orphans {
		found = found || o.Path == target
	}
	if !found {
		t.Skipf("scan did not report the symlinked file: %+v", orphans)
	}
	got := ts.deletePaths(t, target)
	if got.Results[0].Deleted || !strings.Contains(got.Results[0].Error, "parent directory") {
		t.Errorf("file behind a symlinked directory: %+v, want refused by the parent check", got.Results[0])
	}
	if _, err := os.Stat(filepath.Join(elsewhere, "x.jpg")); err != nil {
		t.Errorf("file outside data dir removed: %v", err)
	}
}

func TestStorageOrphanDeleteConflictsWithBackup(t *testing.T) {
	ts := newStorageTestServer(t)
	orphan := filepath.Join(ts.dataDir, "uploads", "b.png")
	writeOld(t, orphan)
	ts.scan(t)
	ts.s.backup = &portableBackup{dataDir: ts.dataDir}
	ts.s.backup.mu.Lock()
	payload, _ := json.Marshal(map[string][]string{"paths": {orphan}})
	w := ts.do(t, http.MethodPost, "/api/admin/storage/orphans/delete", string(payload))
	ts.s.backup.mu.Unlock()
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "backup or restore in progress") {
		t.Fatalf("status = %d body = %s, want 409", w.Code, w.Body.String())
	}
	if _, err := os.Stat(orphan); err != nil {
		t.Errorf("file removed during backup: %v", err)
	}
	// Once the lock is free the delete goes through.
	if got := ts.deletePaths(t, orphan); !got.Results[0].Deleted {
		t.Errorf("delete after backup = %+v", got.Results[0])
	}
}

func TestStorageOrphanDeleteFailsClosedOnReferenceError(t *testing.T) {
	ts := newStorageTestServer(t)
	orphan := filepath.Join(ts.dataDir, "uploads", "b.png")
	writeOld(t, orphan)
	ts.scan(t)
	ts.s.audiobooks = failingStoragePathsStore{}
	payload, _ := json.Marshal(map[string][]string{"paths": {orphan}})
	w := ts.do(t, http.MethodPost, "/api/admin/storage/orphans/delete", string(payload))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500: %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(orphan); err != nil {
		t.Errorf("file removed despite reference error: %v", err)
	}
}

type failingStoragePathsStore struct{ audiobookStore }

func (failingStoragePathsStore) ListStoragePaths(context.Context) ([]audiobookStoragePath, error) {
	return nil, errors.New("db down")
}

func TestStorageEndpointWithoutPostgres(t *testing.T) {
	ts := newStorageTestServer(t)
	meme, err := ts.store.Create(accessor.CreateInput{
		File:        strings.NewReader("png bytes!"),
		Header:      textproto.MIMEHeader{"Content-Type": []string{"image/png"}},
		Filename:    "big.png",
		ContentType: "image/png",
	})
	if err != nil {
		t.Fatal(err)
	}
	w := ts.do(t, http.MethodGet, "/api/admin/storage", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if string(body["uploads"]) != "[]" {
		t.Errorf("uploads = %s, want []", body["uploads"])
	}
	var dirs []storageDirUsage
	if err := json.Unmarshal(body["dirs"], &dirs); err != nil || len(dirs) != len(storageBuckets) {
		t.Errorf("dirs = %s (%v)", body["dirs"], err)
	}
	var largest []storageLargeFile
	if err := json.Unmarshal(body["largest"], &largest); err != nil {
		t.Fatal(err)
	}
	if len(largest) != 1 || largest[0].Kind != "meme" || largest[0].ID != meme.ID || largest[0].Bytes != meme.SizeBytes {
		t.Errorf("largest = %+v", largest)
	}
	if _, ok := body["generated_at"]; !ok {
		t.Error("generated_at missing")
	}
	if _, ok := body["disk"]; !ok {
		t.Error("disk key missing")
	}

	// Cached for 5 minutes; refresh=1 bypasses.
	writeSized(t, filepath.Join(ts.dataDir, "downloads", "d.bin"), 9)
	cached := ts.do(t, http.MethodGet, "/api/admin/storage", "")
	if cached.Body.String() != w.Body.String() {
		t.Error("second GET was not served from cache")
	}
	fresh := ts.do(t, http.MethodGet, "/api/admin/storage?refresh=1", "")
	if fresh.Body.String() == w.Body.String() {
		t.Error("refresh=1 served the cached report")
	}

	if got := ts.do(t, http.MethodPost, "/api/admin/storage", ""); got.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /api/admin/storage = %d", got.Code)
	}
}

func TestAdminDiscardUploadWithoutPostgres(t *testing.T) {
	ts := newStorageTestServer(t)
	w := ts.do(t, http.MethodDelete, "/api/admin/audiobooks/uploads/x", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"unavailable"`) {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	if got := ts.do(t, http.MethodGet, "/api/admin/audiobooks/uploads/x", ""); got.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET = %d, want 405", got.Code)
	}
}

func TestAdminDiscardUploadAndInProgressList(t *testing.T) {
	ts := newStorageTestServer(t)
	store, _ := newIsolatedAudiobookStore(t)
	ts.s.audiobooks = store
	ctx := context.Background()
	first, err := store.CreateUpload(ctx, audiobookUploadSeed{UserID: "u1", Title: "First", Filename: "a.mp3", MimeType: "audio/mpeg", TotalBytes: 10})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateUpload(ctx, audiobookUploadSeed{UserID: "u2", Title: "Second", Filename: "b.mp3", MimeType: "audio/mpeg", TotalBytes: 20})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE audiobook_uploads SET updated_at = NOW() - INTERVAL '1 hour' WHERE id = $1`, first.ID); err != nil {
		t.Fatal(err)
	}
	uploads, err := store.ListInProgressUploads(ctx)
	if err != nil || len(uploads) != 2 || uploads[0].ID != second.ID || uploads[1].ID != first.ID {
		t.Fatalf("uploads = %+v, %v; want second then first", uploads, err)
	}
	got, err := store.GetUploadByID(ctx, first.ID)
	if err != nil || got.UserID != "u1" || got.TempPath != first.TempPath {
		t.Fatalf("GetUploadByID = %+v, %v", got, err)
	}
	if _, err := store.GetUploadByID(ctx, "nope"); !errors.Is(err, ErrAudiobookUploadNotFound) {
		t.Fatalf("missing upload err = %v", err)
	}

	report := ts.do(t, http.MethodGet, "/api/admin/storage?refresh=1", "")
	var body struct {
		Uploads []adminUploadInProgress `json:"uploads"`
	}
	if err := json.Unmarshal(report.Body.Bytes(), &body); err != nil || len(body.Uploads) != 2 || body.Uploads[0].ID != second.ID || body.Uploads[0].TotalBytes != 20 {
		t.Fatalf("storage uploads = %+v (%v): %s", body.Uploads, err, report.Body.String())
	}

	w := ts.do(t, http.MethodDelete, "/api/admin/audiobooks/uploads/"+first.ID, "")
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(first.TempPath); !os.IsNotExist(err) {
		t.Errorf("temp file still exists: %v", err)
	}
	if _, err := store.GetUploadByID(ctx, first.ID); !errors.Is(err, ErrAudiobookUploadNotFound) {
		t.Errorf("row still present: %v", err)
	}
	if _, err := os.Stat(second.TempPath); err != nil {
		t.Errorf("other upload touched: %v", err)
	}
	if w := ts.do(t, http.MethodDelete, "/api/admin/audiobooks/uploads/"+first.ID, ""); w.Code != http.StatusNotFound {
		t.Errorf("second delete = %d, want 404", w.Code)
	}
}

func TestCollectStorageReferencesRejectsMismatchedUploadDir(t *testing.T) {
	store, err := accessor.NewMemeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{config: Config{DataDir: t.TempDir()}, managers: manager.NewMemeManager(store)}
	if _, _, err := s.collectStorageReferences(context.Background()); err == nil {
		t.Fatal("expected an error when the upload dir is outside the data dir")
	}
}
