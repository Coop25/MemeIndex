package client

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

type adminAudiobooksBody struct {
	Items    []adminAudiobookRow `json:"items"`
	Total    int                 `json:"total"`
	Page     int                 `json:"page"`
	PageSize int                 `json:"page_size"`
}

func libraryTestServer(t *testing.T) (*storageTestServer, *postgresAudiobookStore) {
	t.Helper()
	store, _ := newIsolatedAudiobookStore(t)
	ts := newStorageTestServer(t)
	ts.s.audiobooks = store
	return ts, store
}

func insertLibraryBook(t *testing.T, store *postgresAudiobookStore, title string, size int64, createdAt time.Time) string {
	t.Helper()
	id := insertActivityBook(t, store, title, "cover.jpg", "[]", "")
	if _, err := store.pool.Exec(context.Background(), `UPDATE audiobooks SET size_bytes = $2, created_at = $3 WHERE id = $1`, id, size, createdAt); err != nil {
		t.Fatal(err)
	}
	return id
}

func getLibrary(t *testing.T, ts *storageTestServer, query string) (int, adminAudiobooksBody) {
	t.Helper()
	w := ts.do(t, http.MethodGet, "/api/admin/audiobooks"+query, "")
	var body adminAudiobooksBody
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v: %s", err, w.Body.String())
		}
	}
	return w.Code, body
}

func TestAdminAudiobooksSortBySize(t *testing.T) {
	ts, store := libraryTestServer(t)
	now := time.Now()
	insertLibraryBook(t, store, "A", 100, now)
	insertLibraryBook(t, store, "B", 300, now)
	insertLibraryBook(t, store, "C", 200, now)
	code, body := getLibrary(t, ts, "?sort=size&dir=desc")
	if code != 200 || len(body.Items) != 3 {
		t.Fatalf("code %d items %d", code, len(body.Items))
	}
	if body.Items[0].SizeBytes != 300 || body.Items[1].SizeBytes != 200 || body.Items[2].SizeBytes != 100 {
		t.Errorf("sizes = %d,%d,%d", body.Items[0].SizeBytes, body.Items[1].SizeBytes, body.Items[2].SizeBytes)
	}
	if body.Items[0].Format != "M4B" {
		t.Errorf("format = %q", body.Items[0].Format)
	}
}

func TestAdminAudiobooksFilterNoCover(t *testing.T) {
	ts, store := libraryTestServer(t)
	insertActivityBook(t, store, "Covered", "c.jpg", "[]", "")
	bare := insertActivityBook(t, store, "Bare", "", "[]", "")
	code, body := getLibrary(t, ts, "?filter=no-cover")
	if code != 200 || body.Total != 1 || len(body.Items) != 1 || body.Items[0].ID != bare || body.Items[0].HasCover {
		t.Fatalf("code %d body %+v", code, body)
	}
}

func TestAdminAudiobooksFilterNotConverted(t *testing.T) {
	ts, store := libraryTestServer(t)
	insertActivityBook(t, store, "M4B", "c.jpg", "[]", "")
	mp3 := insertActivityBook(t, store, "MP3", "c.jpg", "[]", "")
	if _, err := store.pool.Exec(context.Background(), `UPDATE audiobooks SET storage_path = '/tmp/x.MP3' WHERE id = $1`, mp3); err != nil {
		t.Fatal(err)
	}
	_, body := getLibrary(t, ts, "?filter=not-converted")
	if body.Total != 1 || body.Items[0].ID != mp3 || body.Items[0].Format != "MP3" {
		t.Fatalf("body %+v", body)
	}
	// conversion-failed with no markers on disk matches nothing.
	_, body = getLibrary(t, ts, "?filter=conversion-failed")
	if body.Total != 0 || len(body.Items) != 0 {
		t.Fatalf("failed body %+v", body)
	}
}

func TestAdminAudiobooksNotConvertedExcludesFailed(t *testing.T) {
	ts, store := libraryTestServer(t)
	ctx := context.Background()
	dir := t.TempDir()
	var mp3IDs, mp3Paths []string
	for _, name := range []string{"a.mp3", "b.mp3", "c.mp3"} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		id := insertActivityBook(t, store, name, "c.jpg", "[]", "")
		if _, err := store.pool.Exec(ctx, `UPDATE audiobooks SET storage_path = $2, mime_type = 'audio/mpeg' WHERE id = $1`, id, p); err != nil {
			t.Fatal(err)
		}
		mp3IDs = append(mp3IDs, id)
		mp3Paths = append(mp3Paths, p)
	}
	insertActivityBook(t, store, "M4B", "c.jpg", "[]", "") // already converted
	recordAudiobookRemuxFailure(mp3Paths[0], os.ErrInvalid)

	_, body := getLibrary(t, ts, "?filter=not-converted")
	got := []string{}
	for _, it := range body.Items {
		got = append(got, it.ID)
	}
	want := []string{mp3IDs[1], mp3IDs[2]}
	sort.Strings(got)
	sort.Strings(want)
	if body.Total != 2 || len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("not-converted total %d ids %v, want %v", body.Total, got, want)
	}
	// Paging placeholders must follow the filter args.
	_, paged := getLibrary(t, ts, "?filter=not-converted&page_size=1&page=2")
	if paged.Total != 2 || len(paged.Items) != 1 {
		t.Fatalf("paged not-converted %+v", paged)
	}

	_, failed := getLibrary(t, ts, "?filter=conversion-failed")
	if failed.Total != 1 || len(failed.Items) != 1 || failed.Items[0].ID != mp3IDs[0] {
		t.Fatalf("conversion-failed %+v", failed)
	}

	activity, err := ts.s.cachedAudiobookActivity(ctx)
	if err != nil || activity == nil {
		t.Fatalf("activity %v err %v", activity, err)
	}
	if activity.Health.AwaitingConversion != body.Total {
		t.Errorf("dashboard awaiting = %d, not-converted total = %d", activity.Health.AwaitingConversion, body.Total)
	}
}

func TestAdminAudiobooksPaging(t *testing.T) {
	ts, store := libraryTestServer(t)
	now := time.Now()
	for i := 0; i < 5; i++ {
		insertLibraryBook(t, store, "Book", int64(i+1), now.Add(time.Duration(i)*time.Minute))
	}
	code, body := getLibrary(t, ts, "?page_size=2&page=3")
	if code != 200 || len(body.Items) != 1 || body.Total != 5 || body.Page != 3 || body.PageSize != 2 {
		t.Fatalf("code %d body %+v", code, body)
	}
}

func TestAdminAudiobooksRejectsUnknownFilter(t *testing.T) {
	ts, _ := libraryTestServer(t)
	if code, _ := getLibrary(t, ts, "?filter=bogus"); code != http.StatusBadRequest {
		t.Errorf("code = %d, want 400", code)
	}
}

func TestAdminAudiobooksSortInjection(t *testing.T) {
	ts, store := libraryTestServer(t)
	now := time.Now()
	old := insertLibraryBook(t, store, "Old", 1, now.Add(-time.Hour))
	newer := insertLibraryBook(t, store, "New", 1, now)
	code, body := getLibrary(t, ts, "?sort=title%3BDROP%20TABLE%20audiobooks&dir=asc")
	if code != 200 || len(body.Items) != 2 || body.Items[0].ID != newer || body.Items[1].ID != old {
		t.Fatalf("code %d body %+v", code, body)
	}
	var n int
	if err := store.pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM audiobooks`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("table damaged: n=%d err=%v", n, err)
	}
}

func TestAdminAudiobooksListenerStats(t *testing.T) {
	ts, store := libraryTestServer(t)
	id := insertActivityBook(t, store, "Listened", "c.jpg", "[]", "")
	now := time.Now().UTC()
	insertListening(t, store, "u1", id, now, 100)
	insertListening(t, store, "u2", id, now.Add(-2*24*time.Hour), 50)
	insertListening(t, store, "u3", id, now.Add(-60*24*time.Hour), 25)
	_, body := getLibrary(t, ts, "?sort=listeners&dir=desc")
	if len(body.Items) != 1 || body.Items[0].ActiveListeners != 2 || body.Items[0].SecondsListened != 175 {
		t.Fatalf("body %+v", body)
	}
	if body.Items[0].UploaderName != "u-admin" {
		t.Errorf("uploader name = %q, want id fallback", body.Items[0].UploaderName)
	}
}

func TestAdminAudiobooksWithoutPostgres(t *testing.T) {
	ts := newStorageTestServer(t)
	w := ts.do(t, http.MethodGet, "/api/admin/audiobooks", "")
	if w.Code != 200 || w.Body.String() != "{\"state\":\"unavailable\"}\n" {
		t.Errorf("code %d body %q", w.Code, w.Body.String())
	}
	if w := ts.do(t, http.MethodPost, "/api/admin/audiobooks", "{}"); w.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST code = %d", w.Code)
	}
}

func TestAdminWindowStart(t *testing.T) {
	now := time.Date(2026, 11, 5, 23, 59, 0, 0, time.FixedZone("x", -8*3600)) // 2026-11-06 07:59 UTC
	want := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	if got := adminWindowStart(now); !got.Equal(want) {
		t.Errorf("adminWindowStart = %v, want %v", got, want)
	}
}

func TestAdminAudiobooksListenerWindowBoundary(t *testing.T) {
	ts, store := libraryTestServer(t)
	id := insertActivityBook(t, store, "Listened", "c.jpg", "[]", "")
	today := time.Now().UTC()
	insertListening(t, store, "in", id, today.AddDate(0, 0, -29), 10)
	insertListening(t, store, "out", id, today.AddDate(0, 0, -30), 10)
	_, body := getLibrary(t, ts, "")
	if len(body.Items) != 1 || body.Items[0].ActiveListeners != 1 {
		t.Fatalf("active listeners = %+v, want 1 (today-29 counts, today-30 does not)", body.Items)
	}
}

func TestAdminAudiobooksHugePage(t *testing.T) {
	ts, store := libraryTestServer(t)
	insertLibraryBook(t, store, "A", 1, time.Now())
	code, body := getLibrary(t, ts, "?page=9223372036854775807")
	if code != http.StatusOK || len(body.Items) != 0 {
		t.Fatalf("code %d body %+v", code, body)
	}
}
