package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAdminUserTotals(t *testing.T) {
	store, _ := newIsolatedAudiobookStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	b1 := insertActivityBook(t, store, "One", "", "[]", "")
	b2 := insertActivityBook(t, store, "Two", "", "[]", "")
	if _, err := store.pool.Exec(ctx, `UPDATE audiobooks SET uploaded_by_user_id = 'u1' WHERE id = ANY($1)`, []string{b1, b2}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO audiobook_progress (audiobook_id, user_id, position_seconds, finished_at) VALUES ($1, 'u1', 60, NOW()), ($2, 'u1', 10, NULL)`, b1, b2); err != nil {
		t.Fatal(err)
	}
	insertListening(t, store, "u1", b1, now, 600)
	insertListening(t, store, "u1", b1, now.AddDate(0, 0, -40), 900)

	totals, err := store.AdminUserAudiobookTotals(ctx, adminWindowStart(now))
	if err != nil {
		t.Fatalf("AdminUserAudiobookTotals: %v", err)
	}
	want := adminUserAudiobookTotals{Uploaded: 2, Seconds30d: 600, SecondsAllTime: 1500, Finished: 1}
	if got := totals["u1"]; got != want {
		t.Fatalf("u1 totals = %+v, want %+v", got, want)
	}
}

type userActivityUsers struct {
	securityUsers
	records []managedUserRecord
}

func (u *userActivityUsers) ListUsers(context.Context) ([]managedUserRecord, error) {
	return u.records, nil
}

func TestUserActivityWithoutPostgres(t *testing.T) {
	s, store, meme := newShareTestServer(t)
	s.config.DataDir = t.TempDir()
	perms := authPermissions{CanView: true, CanManageUsers: true}
	users := &userActivityUsers{
		securityUsers: securityUsers{permissions: perms},
		records: []managedUserRecord{
			{UserID: "u1", Username: "alice", DisplayName: "Alice", LastActiveAt: 1700000000},
			{UserID: "u2", Username: "bob"},
		},
	}
	s.users = users
	s.auth = newAuthService(DiscordAuthConfig{ClientID: "c", ClientSecret: "s", RedirectURL: "https://example.com/auth/callback", SessionSecret: "test"}, users)
	if _, err := store.SetFavorite("u1", meme.ID, true); err != nil {
		t.Fatal(err)
	}
	token, err := s.auth.issueSessionToken(authSession{UserID: "admin", SessionVersion: 1, ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "https://example.com/api/admin/user-activity", nil)
	w := httptest.NewRecorder()
	s.auth.setSessionCookie(w, r, token, time.Now().Add(time.Hour))
	r.AddCookie(w.Result().Cookies()[0])
	w = httptest.NewRecorder()
	s.Routes().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Users []adminUserActivity `json:"users"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Users) != 2 {
		t.Fatalf("users = %+v", body.Users)
	}
	byID := map[string]adminUserActivity{}
	for _, u := range body.Users {
		byID[u.UserID] = u
	}
	alice := byID["u1"]
	if alice.DisplayName != "Alice" || alice.LastActiveAt != 1700000000 || alice.Favorites != 1 {
		t.Errorf("alice = %+v", alice)
	}
	for id, u := range byID {
		if u.MemesUploaded != 0 || u.AudiobooksUploaded != 0 || u.Seconds30d != 0 || u.SecondsAllTime != 0 || u.BooksFinished != 0 {
			t.Errorf("%s has non-zero audit/audiobook fields: %+v", id, u)
		}
	}
	if byID["u2"].Favorites != 0 {
		t.Errorf("bob favorites = %d", byID["u2"].Favorites)
	}
}

func TestAdminUserTotalsWindowBoundary(t *testing.T) {
	store, _ := newIsolatedAudiobookStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	b1 := insertActivityBook(t, store, "One", "", "[]", "")
	insertListening(t, store, "in", b1, now.AddDate(0, 0, -29), 100)
	insertListening(t, store, "out", b1, now.AddDate(0, 0, -30), 100)
	totals, err := store.AdminUserAudiobookTotals(ctx, adminWindowStart(now))
	if err != nil {
		t.Fatal(err)
	}
	if totals["in"].Seconds30d != 100 || totals["out"].Seconds30d != 0 || totals["out"].SecondsAllTime != 100 {
		t.Errorf("totals = %+v", totals)
	}
}
