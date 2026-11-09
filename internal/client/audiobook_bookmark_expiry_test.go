package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// bookmarkExpiryStore keeps bookmarks in memory - just enough of the store
// for the bookmark list/use handlers and the expiry sweeper.
type bookmarkExpiryStore struct {
	audiobookStore
	bookmarks     []audiobookBookmark
	touched       []string
	deleteCutoffs []time.Time
	// books, when set, is every audiobook that exists; nil means any id does.
	books map[string]audiobookRecord
}

func (s *bookmarkExpiryStore) ListBookmarks(_ context.Context, audiobookID, userID string) ([]audiobookBookmark, error) {
	out := []audiobookBookmark{}
	for _, b := range s.bookmarks {
		if b.AudiobookID == audiobookID && b.UserID == userID {
			out = append(out, b)
		}
	}
	return out, nil
}

// Mirrors the Postgres WHERE clause: same user, same book, and (unless
// expiry is off) used after the cutoff.
func (s *bookmarkExpiryStore) TouchBookmark(_ context.Context, audiobookID, id, userID string, usedSince time.Time) (audiobookBookmark, error) {
	for i, b := range s.bookmarks {
		if b.ID == id && b.UserID == userID && b.AudiobookID == audiobookID && (usedSince.IsZero() || b.LastUsedAt.After(usedSince)) {
			s.bookmarks[i].LastUsedAt = time.Now()
			s.touched = append(s.touched, id)
			return s.bookmarks[i], nil
		}
	}
	return audiobookBookmark{}, ErrAudiobookBookmarkNotFound
}

func (s *bookmarkExpiryStore) DeleteExpiredBookmarks(_ context.Context, cutoff time.Time) (int64, error) {
	s.deleteCutoffs = append(s.deleteCutoffs, cutoff)
	return 0, nil
}

const sixMonths = 182 * 24 * time.Hour

func TestListAudiobookBookmarksHidesExpiredAndReportsExpiry(t *testing.T) {
	now := time.Now()
	store := &bookmarkExpiryStore{bookmarks: []audiobookBookmark{
		{ID: "fresh", AudiobookID: "book", UserID: "", LastUsedAt: now.Add(-24 * time.Hour)},
		{ID: "stale", AudiobookID: "book", UserID: "", LastUsedAt: now.Add(-sixMonths - time.Hour)},
	}}
	s := &Server{audiobooks: store, config: Config{AudiobookBookmarkExpiry: sixMonths}}

	rec := httptest.NewRecorder()
	s.listAudiobookBookmarks(rec, httptest.NewRequest(http.MethodGet, "/api/audiobooks/book/bookmarks", nil), "book")

	var body struct {
		Bookmarks []struct {
			ID        string     `json:"id"`
			ExpiresAt *time.Time `json:"expires_at"`
		} `json:"bookmarks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v (%s)", err, rec.Body.String())
	}
	if len(body.Bookmarks) != 1 || body.Bookmarks[0].ID != "fresh" {
		t.Fatalf("bookmarks = %+v, want only the fresh one", body.Bookmarks)
	}
	want := store.bookmarks[0].LastUsedAt.Add(sixMonths)
	if got := body.Bookmarks[0].ExpiresAt; got == nil || !got.Equal(want) {
		t.Errorf("expires_at = %v, want %v", got, want)
	}
}

func TestListAudiobookBookmarksKeepsEverythingWhenExpiryDisabled(t *testing.T) {
	store := &bookmarkExpiryStore{bookmarks: []audiobookBookmark{
		{ID: "ancient", AudiobookID: "book", LastUsedAt: time.Now().Add(-10 * sixMonths)},
	}}
	s := &Server{audiobooks: store}

	rec := httptest.NewRecorder()
	s.listAudiobookBookmarks(rec, httptest.NewRequest(http.MethodGet, "/api/audiobooks/book/bookmarks", nil), "book")

	var body struct {
		Bookmarks []map[string]any `json:"bookmarks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Bookmarks) != 1 {
		t.Fatalf("got %d bookmarks, want 1", len(body.Bookmarks))
	}
	if v, ok := body.Bookmarks[0]["expires_at"]; !ok || v != nil {
		t.Errorf("expires_at = %v (present %v), want null", v, ok)
	}
}

func TestUseAudiobookBookmarkRestartsExpiry(t *testing.T) {
	store := &bookmarkExpiryStore{bookmarks: []audiobookBookmark{
		{ID: "b1", AudiobookID: "book", LastUsedAt: time.Now().Add(-100 * 24 * time.Hour)},
	}}
	s := &Server{audiobooks: store, config: Config{AudiobookBookmarkExpiry: sixMonths}}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/audiobooks/book/bookmarks/b1/use", nil)
	s.useAudiobookBookmark(rec, req, "book", "b1")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if len(store.touched) != 1 || store.touched[0] != "b1" {
		t.Errorf("touched = %v, want [b1]", store.touched)
	}

	rec = httptest.NewRecorder()
	s.useAudiobookBookmark(rec, httptest.NewRequest(http.MethodPost, "/api/audiobooks/book/bookmarks/missing/use", nil), "book", "missing")
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown bookmark: status = %d, want 404", rec.Code)
	}
}

func TestUseAudiobookBookmarkRejectsOtherBookAndExpired(t *testing.T) {
	store := &bookmarkExpiryStore{bookmarks: []audiobookBookmark{
		{ID: "b1", AudiobookID: "hidden-book", LastUsedAt: time.Now()},
		{ID: "old", AudiobookID: "book", LastUsedAt: time.Now().Add(-sixMonths - time.Hour)},
	}}
	s := &Server{audiobooks: store, config: Config{AudiobookBookmarkExpiry: sixMonths}}

	// Access is checked against the book in the URL, so a bookmark on a
	// different book must not be reachable through it.
	rec := httptest.NewRecorder()
	s.useAudiobookBookmark(rec, httptest.NewRequest(http.MethodPost, "/api/audiobooks/book/bookmarks/b1/use", nil), "book", "b1")
	if rec.Code != http.StatusNotFound {
		t.Errorf("bookmark from another book: status = %d, want 404", rec.Code)
	}

	// Expired but not yet swept: the list already hides it, so a jump from
	// a stale list must not bring it back.
	rec = httptest.NewRecorder()
	s.useAudiobookBookmark(rec, httptest.NewRequest(http.MethodPost, "/api/audiobooks/book/bookmarks/old/use", nil), "book", "old")
	if rec.Code != http.StatusNotFound {
		t.Errorf("expired bookmark: status = %d, want 404", rec.Code)
	}
	if len(store.touched) != 0 {
		t.Errorf("touched = %v, want none", store.touched)
	}
}

func TestSweepExpiredAudiobookBookmarksUsesConfiguredWindow(t *testing.T) {
	store := &bookmarkExpiryStore{}
	s := &Server{audiobooks: store, config: Config{AudiobookBookmarkExpiry: sixMonths}}
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)

	s.sweepExpiredAudiobookBookmarks(context.Background(), now)
	if len(store.deleteCutoffs) != 1 || !store.deleteCutoffs[0].Equal(now.Add(-sixMonths)) {
		t.Errorf("cutoffs = %v, want [%v]", store.deleteCutoffs, now.Add(-sixMonths))
	}
}

func TestNextAudiobookBookmarkSweepIsNextMidnight(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("no tzdata: %v", err)
	}
	cases := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{"mid-afternoon", time.Date(2026, 11, 8, 15, 30, 0, 0, time.UTC), time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)},
		{"exactly midnight goes to the next one", time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)},
		{"month rollover", time.Date(2026, 11, 30, 23, 59, 0, 0, time.UTC), time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)},
		// The night clocks fall back is 25 hours long - still lands on midnight.
		{"across DST", time.Date(2026, 10, 31, 12, 0, 0, 0, ny), time.Date(2026, 11, 1, 0, 0, 0, 0, ny)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := nextAudiobookBookmarkSweep(tc.now); !got.Equal(tc.want) {
				t.Errorf("next sweep after %v = %v, want %v", tc.now, got, tc.want)
			}
		})
	}
}

func TestLoadConfigBookmarkExpiryDefaultsToSixMonths(t *testing.T) {
	t.Setenv("MEMEINDEX_ALLOW_ANONYMOUS", "true")
	// Unset for this test even if the developer's shell exports it;
	// t.Setenv first so the original value is restored afterwards.
	t.Setenv("MEMEINDEX_AUDIOBOOK_BOOKMARK_EXPIRY_DAYS", "")
	os.Unsetenv("MEMEINDEX_AUDIOBOOK_BOOKMARK_EXPIRY_DAYS")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.AudiobookBookmarkExpiry != sixMonths {
		t.Errorf("default expiry = %v, want %v", cfg.AudiobookBookmarkExpiry, sixMonths)
	}

	t.Setenv("MEMEINDEX_AUDIOBOOK_BOOKMARK_EXPIRY_DAYS", "0")
	cfg, err = LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.AudiobookBookmarkExpiry != 0 {
		t.Errorf("expiry with 0 days = %v, want disabled", cfg.AudiobookBookmarkExpiry)
	}

	// Past what time.Duration can hold - must clamp, not wrap around.
	t.Setenv("MEMEINDEX_AUDIOBOOK_BOOKMARK_EXPIRY_DAYS", "200000")
	cfg, err = LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if want := time.Duration(maxAudiobookBookmarkExpiryDays) * 24 * time.Hour; cfg.AudiobookBookmarkExpiry != want {
		t.Errorf("expiry with 200000 days = %v, want clamped to %v", cfg.AudiobookBookmarkExpiry, want)
	}
}

// Mirrors the Postgres replace: same user, book and label, within the window.
func (s *bookmarkExpiryStore) CreateBookmarkReplacingNearby(_ context.Context, audiobookID, userID string, positionSeconds float64, label string, windowSeconds float64) (audiobookBookmark, []string, error) {
	kept := s.bookmarks[:0]
	replaced := []string{}
	for _, b := range s.bookmarks {
		diff := b.PositionSeconds - positionSeconds
		if diff < 0 {
			diff = -diff
		}
		if b.AudiobookID == audiobookID && b.UserID == userID && b.Label == label && diff <= windowSeconds {
			replaced = append(replaced, b.ID)
			continue
		}
		kept = append(kept, b)
	}
	s.bookmarks = kept
	created := audiobookBookmark{ID: "new", AudiobookID: audiobookID, UserID: userID, PositionSeconds: positionSeconds, Label: label, CreatedAt: time.Now(), LastUsedAt: time.Now()}
	s.bookmarks = append(s.bookmarks, created)
	return created, replaced, nil
}

func (s *bookmarkExpiryStore) CreateBookmark(_ context.Context, audiobookID, userID string, positionSeconds float64, label string) (audiobookBookmark, error) {
	created := audiobookBookmark{ID: "new", AudiobookID: audiobookID, UserID: userID, PositionSeconds: positionSeconds, Label: label, CreatedAt: time.Now(), LastUsedAt: time.Now()}
	s.bookmarks = append(s.bookmarks, created)
	return created, nil
}

func TestSleepTimerBookmarkReplacesNearbySleepTimerBookmarks(t *testing.T) {
	now := time.Now()
	store := &bookmarkExpiryStore{bookmarks: []audiobookBookmark{
		{ID: "near-before", AudiobookID: "book", Label: audiobookSleepTimerBookmarkLabel, PositionSeconds: 10000 - 19*60, LastUsedAt: now},
		{ID: "near-after", AudiobookID: "book", Label: audiobookSleepTimerBookmarkLabel, PositionSeconds: 10000 + 60, LastUsedAt: now},
		{ID: "far", AudiobookID: "book", Label: audiobookSleepTimerBookmarkLabel, PositionSeconds: 10000 - 21*60, LastUsedAt: now},
		{ID: "by-hand", AudiobookID: "book", Label: "", PositionSeconds: 10000, LastUsedAt: now},
		{ID: "other-book", AudiobookID: "book-2", Label: audiobookSleepTimerBookmarkLabel, PositionSeconds: 10000, LastUsedAt: now},
	}}
	s := &Server{audiobooks: store}

	rec := httptest.NewRecorder()
	body := strings.NewReader(`{"positionSeconds":10000,"label":"anything","sleepTimer":true}`)
	s.createAudiobookBookmark(rec, httptest.NewRequest(http.MethodPost, "/api/audiobooks/book/bookmarks", body), "book")
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	var got struct {
		Label    string   `json:"label"`
		Replaced []string `json:"replaced_bookmark_ids"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Label != audiobookSleepTimerBookmarkLabel {
		t.Errorf("label = %q, want %q", got.Label, audiobookSleepTimerBookmarkLabel)
	}
	if strings.Join(got.Replaced, ",") != "near-before,near-after" {
		t.Errorf("replaced = %v, want [near-before near-after]", got.Replaced)
	}
	var left []string
	for _, b := range store.bookmarks {
		left = append(left, b.ID)
	}
	if strings.Join(left, ",") != "far,by-hand,other-book,new" {
		t.Errorf("remaining = %v", left)
	}
}

func TestPlainBookmarkReplacesNothing(t *testing.T) {
	store := &bookmarkExpiryStore{bookmarks: []audiobookBookmark{
		{ID: "sleep", AudiobookID: "book", Label: audiobookSleepTimerBookmarkLabel, PositionSeconds: 100, LastUsedAt: time.Now()},
	}}
	s := &Server{audiobooks: store}

	rec := httptest.NewRecorder()
	s.createAudiobookBookmark(rec, httptest.NewRequest(http.MethodPost, "/api/audiobooks/book/bookmarks", strings.NewReader(`{"positionSeconds":100}`)), "book")
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d", rec.Code)
	}
	if len(store.bookmarks) != 2 {
		t.Errorf("bookmarks = %d, want both kept", len(store.bookmarks))
	}
	if !strings.Contains(rec.Body.String(), `"replaced_bookmark_ids":[]`) {
		t.Errorf("body = %s, want an empty replaced list", rec.Body.String())
	}
}

func (s *bookmarkExpiryStore) GetAudiobook(_ context.Context, id string) (audiobookRecord, error) {
	if s.books == nil {
		return audiobookRecord{ID: id}, nil
	}
	record, ok := s.books[id]
	if !ok {
		return audiobookRecord{}, ErrAudiobookNotFound
	}
	return record, nil
}

func TestBookmarkEndpointsHonorTagRestrictionAndMissingBooks(t *testing.T) {
	store := &bookmarkExpiryStore{
		books: map[string]audiobookRecord{
			"hidden": {ID: "hidden", Tags: []string{"adult"}},
		},
		bookmarks: []audiobookBookmark{
			{ID: "b1", AudiobookID: "hidden", UserID: "u1", LastUsedAt: time.Now()},
		},
	}
	s := &Server{audiobooks: store, auth: &authService{}}
	restricted := authSession{UserID: "u1", Permissions: authPermissions{AudiobookTagRestriction: []string{"kids"}}}
	req := func(method, path, body string) *http.Request {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		return r.WithContext(contextWithSession(r.Context(), restricted))
	}

	calls := []struct {
		name string
		run  func(*httptest.ResponseRecorder)
	}{
		{"list hidden", func(rec *httptest.ResponseRecorder) {
			s.listAudiobookBookmarks(rec, req(http.MethodGet, "/api/audiobooks/hidden/bookmarks", ""), "hidden")
		}},
		{"create on hidden", func(rec *httptest.ResponseRecorder) {
			s.createAudiobookBookmark(rec, req(http.MethodPost, "/api/audiobooks/hidden/bookmarks", `{"positionSeconds":5,"sleepTimer":true}`), "hidden")
		}},
		{"use on hidden", func(rec *httptest.ResponseRecorder) {
			s.useAudiobookBookmark(rec, req(http.MethodPost, "/api/audiobooks/hidden/bookmarks/b1/use", ""), "hidden", "b1")
		}},
		{"create on missing book", func(rec *httptest.ResponseRecorder) {
			s.createAudiobookBookmark(rec, req(http.MethodPost, "/api/audiobooks/gone/bookmarks", `{"positionSeconds":5}`), "gone")
		}},
	}
	for _, c := range calls {
		rec := httptest.NewRecorder()
		c.run(rec)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404 (%s)", c.name, rec.Code, rec.Body.String())
		}
	}
	if len(store.bookmarks) != 1 || len(store.touched) != 0 {
		t.Errorf("store changed: bookmarks=%v touched=%v", store.bookmarks, store.touched)
	}
}

func (s *bookmarkExpiryStore) DeleteBookmark(_ context.Context, audiobookID, id, userID string) error {
	for i, b := range s.bookmarks {
		if b.ID == id && b.UserID == userID && b.AudiobookID == audiobookID {
			s.bookmarks = append(s.bookmarks[:i], s.bookmarks[i+1:]...)
			return nil
		}
	}
	return ErrAudiobookBookmarkNotFound
}

func TestDeleteAudiobookBookmarkIsScopedToVisibleBook(t *testing.T) {
	store := &bookmarkExpiryStore{
		books: map[string]audiobookRecord{
			"visible": {ID: "visible", Tags: []string{"kids"}},
			"hidden":  {ID: "hidden", Tags: []string{"adult"}},
		},
		bookmarks: []audiobookBookmark{
			{ID: "on-hidden", AudiobookID: "hidden", UserID: "u1"},
			{ID: "on-visible", AudiobookID: "visible", UserID: "u1"},
		},
	}
	s := &Server{audiobooks: store, auth: &authService{}}
	session := authSession{UserID: "u1", Permissions: authPermissions{AudiobookTagRestriction: []string{"kids"}}}
	del := func(book, bookmark string) int {
		r := httptest.NewRequest(http.MethodDelete, "/api/audiobooks/"+book+"/bookmarks/"+bookmark, nil)
		rec := httptest.NewRecorder()
		s.deleteAudiobookBookmark(rec, r.WithContext(contextWithSession(r.Context(), session)), book, bookmark)
		return rec.Code
	}

	if code := del("hidden", "on-hidden"); code != http.StatusNotFound {
		t.Errorf("through hidden book: status = %d, want 404", code)
	}
	if code := del("visible", "on-hidden"); code != http.StatusNotFound {
		t.Errorf("hidden book's bookmark through a visible book: status = %d, want 404", code)
	}
	if code := del("visible", "on-visible"); code != http.StatusNoContent {
		t.Errorf("own bookmark on visible book: status = %d, want 204", code)
	}
	if len(store.bookmarks) != 1 || store.bookmarks[0].ID != "on-hidden" {
		t.Errorf("remaining = %+v, want only on-hidden", store.bookmarks)
	}
}
