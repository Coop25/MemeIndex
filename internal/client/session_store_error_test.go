package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// failingSessionStore issues sessions normally but fails every user lookup,
// standing in for a Postgres outage after the user already logged in.
type failingSessionStore struct {
	fakeSessionStore
}

func (f *failingSessionStore) GetUser(context.Context, string) (managedUserRecord, bool, error) {
	return managedUserRecord{}, false, errors.New("connection refused")
}

func TestSessionStoreErrorIsNotTreatedAsLoggedOut(t *testing.T) {
	store := &failingSessionStore{fakeSessionStore{version: 1, perms: authPermissions{CanView: true}}}
	a := newRevocationAuthService(store)
	server := &Server{auth: a}

	_, token, err := a.createSession(discordUser{ID: "u1", Username: "user"})
	if err != nil {
		t.Fatalf("createSession: %v", err)
	}

	if _, ok, err := a.resolveSession(requestWithSessionCookie(t, a, token)); ok || !errors.Is(err, errSessionStoreUnavailable) {
		t.Fatalf("resolveSession ok=%v err=%v, want errSessionStoreUnavailable", ok, err)
	}

	reached := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true })

	cases := []struct {
		name    string
		handler http.Handler
		path    string
	}{
		{"api", server.withAPIAuth(next, permissionView), "/api/memes"},
		{"page", server.withPageAuth(next), "/"},
		{"asset", server.withProtectedAssetAuth(next), "/uploads/abc.png"},
		{"session endpoint", http.HandlerFunc(server.handleAuthSession), "/api/auth/session"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reached = false
			req := requestWithSessionCookie(t, a, token)
			req.URL.Path = tc.path
			rec := httptest.NewRecorder()
			tc.handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want 503 (location %q)", rec.Code, rec.Header().Get("Location"))
			}
			if reached {
				t.Fatal("request reached the protected handler without a verified session")
			}
			for _, c := range rec.Result().Cookies() {
				if c.Name == authSessionCookieName && c.MaxAge < 0 {
					t.Fatal("session cookie was cleared on a store error")
				}
			}
		})
	}
}
