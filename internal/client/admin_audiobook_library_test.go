package client

import (
	"context"
	"encoding/json"
	"net/http"
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
