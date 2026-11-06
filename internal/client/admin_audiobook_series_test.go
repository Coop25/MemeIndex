package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
)

func insertSeries(t *testing.T, store *postgresAudiobookStore, id, name string) {
	t.Helper()
	if _, err := store.pool.Exec(context.Background(), `INSERT INTO audiobook_series (id, name) VALUES ($1, $2)`, id, name); err != nil {
		t.Fatalf("insert series: %v", err)
	}
}

func setBookSeries(t *testing.T, store *postgresAudiobookStore, book, series string, seq float64) {
	t.Helper()
	if _, err := store.pool.Exec(context.Background(), `UPDATE audiobooks SET series_id = $2, series_sequence = $3 WHERE id = $1`, book, series, seq); err != nil {
		t.Fatalf("set series: %v", err)
	}
}

func TestMergeSeriesMovesBooksKeepingSequence(t *testing.T) {
	store, _ := newIsolatedAudiobookStore(t)
	ctx := context.Background()
	insertSeries(t, store, "sa", "A")
	insertSeries(t, store, "sb", "B")
	a1 := insertActivityBook(t, store, "a1", "c.jpg", "[]", "")
	a2 := insertActivityBook(t, store, "a2", "c.jpg", "[]", "")
	b3 := insertActivityBook(t, store, "b3", "c.jpg", "[]", "")
	setBookSeries(t, store, a1, "sa", 1)
	setBookSeries(t, store, a2, "sa", 2)
	setBookSeries(t, store, b3, "sb", 3)

	moved, err := store.MergeSeries(ctx, "sa", "sb")
	if err != nil || moved != 2 {
		t.Fatalf("moved=%d err=%v", moved, err)
	}
	rows, err := store.pool.Query(ctx, `SELECT series_sequence FROM audiobooks WHERE series_id = 'sb' ORDER BY series_sequence`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var seqs []float64
	for rows.Next() {
		var f float64
		if err := rows.Scan(&f); err != nil {
			t.Fatal(err)
		}
		seqs = append(seqs, f)
	}
	if len(seqs) != 3 || seqs[0] != 1 || seqs[1] != 2 || seqs[2] != 3 {
		t.Fatalf("sequences = %v", seqs)
	}
	var n int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM audiobook_series WHERE id = 'sa'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("series A remaining = %d err=%v", n, err)
	}
	if _, err := store.MergeSeries(ctx, "sa", "sb"); !errors.Is(err, ErrAudiobookSeriesNotFound) {
		t.Fatalf("missing from: %v", err)
	}
	if _, err := store.MergeSeries(ctx, "sb", "nope"); !errors.Is(err, ErrAudiobookSeriesNotFound) {
		t.Fatalf("missing into: %v", err)
	}
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM audiobook_series WHERE id = 'sb'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("series B should survive failed merge, n=%d err=%v", n, err)
	}
}

func TestMergeSeriesIntoSelfRejected(t *testing.T) {
	ts, store := libraryTestServer(t)
	insertSeries(t, store, "sa", "A")
	insertSeries(t, store, "sb", "B")
	cases := []struct {
		path, body string
		want       int
	}{
		{"/api/admin/audiobook-series/sa/merge", `{"into":"sa"}`, 400},
		{"/api/admin/audiobook-series/sa/merge", `{}`, 400},
		{"/api/admin/audiobook-series/sa/merge", `{"into":"  "}`, 400},
		{"/api/admin/audiobook-series/sa/merge", `not json`, 400},
		{"/api/admin/audiobook-series/sa/merge", `{"into":"missing"}`, 404},
		{"/api/admin/audiobook-series/missing/merge", `{"into":"sb"}`, 404},
		{"/api/admin/audiobook-series/sa", `{"into":"sb"}`, 404},
		{"/api/admin/audiobook-series/sa/other", `{"into":"sb"}`, 404},
		{"/api/admin/audiobook-series/sa/merge", `{"into":"sb"}`, 200},
	}
	for _, c := range cases {
		w := ts.do(t, http.MethodPost, c.path, c.body)
		if w.Code != c.want {
			t.Errorf("POST %s %s = %d, want %d: %s", c.path, c.body, w.Code, c.want, w.Body.String())
		}
	}
	if w := ts.do(t, http.MethodGet, "/api/admin/audiobook-series/sb/merge", ""); w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET merge = %d", w.Code)
	}
}

func TestAdminAudiobookTagUsage(t *testing.T) {
	ts, store := libraryTestServer(t)
	ctx := context.Background()
	ts.s.users = &userActivityUsers{records: []managedUserRecord{{UserID: "u1", Username: "alice", DisplayName: "Alice"}, {UserID: "u2", Username: "bob"}}}
	b1 := insertActivityBook(t, store, "b1", "c.jpg", "[]", "")
	b2 := insertActivityBook(t, store, "b2", "c.jpg", "[]", "")
	var scifi, lonely, memeOnly int64
	for name, dst := range map[string]*int64{"scifi": &scifi, "lonely": &lonely, "memeonly": &memeOnly} {
		if err := store.pool.QueryRow(ctx, `INSERT INTO tags (name) VALUES ($1) RETURNING id`, name).Scan(dst); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO memes (id, original_name, stored_name, file_path, content_type, size_bytes, created_at, updated_at)
		VALUES ('m1', 'cat.png', 'm1.png', '/uploads/m1.png', 'image/png', 1, NOW(), NOW())`); err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := store.pool.Exec(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	// Migration 027 is owned by the user store and is not applied by the
	// isolated helper; create it in this test's schema (an unqualified
	// CREATE ... IF NOT EXISTS would see the shared public copy instead).
	exec(`CREATE TABLE app_user_audiobook_tag_restrictions (
		user_id TEXT NOT NULL,
		tag_id BIGINT NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
		PRIMARY KEY (user_id, tag_id))`)
	exec(`INSERT INTO audiobook_tags VALUES ($1,$2),($3,$2)`, b1, scifi, b2)
	exec(`INSERT INTO audiobook_tags VALUES ($1,$2)`, b1, lonely)
	exec(`INSERT INTO meme_tags VALUES ('m1',$1),('m1',$2)`, scifi, memeOnly)
	exec(`INSERT INTO app_user_audiobook_tag_restrictions VALUES ('u1',$1),('u2',$1)`, scifi)

	w := ts.do(t, http.MethodGet, "/api/admin/audiobook-tags", "")
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Tags []adminAudiobookTagUsage `json:"tags"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Tags) != 2 {
		t.Fatalf("tags = %+v", body.Tags)
	}
	first := body.Tags[0]
	if first.Tag != "scifi" || first.Audiobooks != 2 || first.Memes != 1 || len(first.RestrictedUsers) != 2 {
		t.Fatalf("first = %+v", first)
	}
	if first.RestrictedUsers[0] != (adminRestrictedUser{UserID: "u1", DisplayName: "Alice"}) || first.RestrictedUsers[1] != (adminRestrictedUser{UserID: "u2", DisplayName: "bob"}) {
		t.Fatalf("restricted = %+v", first.RestrictedUsers)
	}
	if second := body.Tags[1]; second.Tag != "lonely" || second.Audiobooks != 1 || second.Memes != 0 || second.RestrictedUsers == nil || len(second.RestrictedUsers) != 0 {
		t.Fatalf("second = %+v", second)
	}
}

func TestAdminSeriesTagRoutesWithoutPostgres(t *testing.T) {
	ts := newStorageTestServer(t)
	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/api/admin/audiobook-series/x/merge"},
		{http.MethodGet, "/api/admin/audiobook-tags"},
	} {
		w := ts.do(t, c.method, c.path, `{"into":"y"}`)
		if w.Code != 200 || w.Body.String() != "{\"state\":\"unavailable\"}\n" {
			t.Errorf("%s = %d %q", c.path, w.Code, w.Body.String())
		}
	}
}
