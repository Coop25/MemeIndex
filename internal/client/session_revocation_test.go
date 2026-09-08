package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// fakeSessionStore is a minimal authUserStore stub focused on the session
// version surface. Unimplemented methods fall through to the embedded nil
// interface and panic if called, which keeps the tests honest about what the
// revocation path actually touches.
type fakeSessionStore struct {
	authUserStore

	mu         sync.Mutex
	version    int64
	perms      authPermissions
	bumpCalls  int
	lastBumpID string
}

func (f *fakeSessionStore) UpsertDiscordProfile(context.Context, discordUser) error { return nil }
func (f *fakeSessionStore) UpsertSessionProfile(context.Context, authClaims) error  { return nil }

func (f *fakeSessionStore) SessionVersion(context.Context, string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.version, nil
}

func (f *fakeSessionStore) BumpSessionVersion(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.version++
	f.bumpCalls++
	f.lastBumpID = id
	return nil
}

func (f *fakeSessionStore) GetUser(_ context.Context, id string) (managedUserRecord, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return managedUserRecord{UserID: id, Permissions: f.perms}, true, nil
}

func newRevocationAuthService(store authUserStore) *authService {
	return newAuthService(DiscordAuthConfig{
		ClientID:        "client",
		ClientSecret:    "secret",
		RedirectURL:     "http://localhost/auth/callback",
		SessionSecret:   "test-session-secret",
		SessionDuration: 24 * time.Hour,
	}, store)
}

func requestWithSessionCookie(t *testing.T, a *authService, token string) *http.Request {
	t.Helper()
	rec := httptest.NewRecorder()
	seed := httptest.NewRequest(http.MethodGet, "http://localhost/api/memes", nil)
	a.setSessionCookie(rec, seed, token, time.Now().Add(time.Hour))

	req := httptest.NewRequest(http.MethodGet, "http://localhost/api/memes", nil)
	for _, cookie := range rec.Result().Cookies() {
		req.AddCookie(cookie)
	}
	return req
}

func TestCreateSessionEmbedsStoredSessionVersion(t *testing.T) {
	store := &fakeSessionStore{version: 7, perms: authPermissions{CanView: true}}
	a := newRevocationAuthService(store)

	_, token, err := a.createSession(discordUser{ID: "u1", Username: "user"})
	if err != nil {
		t.Fatalf("createSession: %v", err)
	}

	claims, ok := a.parseSessionToken(token)
	if !ok {
		t.Fatal("freshly issued token did not parse")
	}
	if claims.SessionVersion != 7 {
		t.Fatalf("token session version = %d, want 7", claims.SessionVersion)
	}
}

func TestSessionFromRequestAcceptsMatchingVersion(t *testing.T) {
	store := &fakeSessionStore{version: 3, perms: authPermissions{CanView: true}}
	a := newRevocationAuthService(store)

	_, token, err := a.createSession(discordUser{ID: "u1", Username: "user"})
	if err != nil {
		t.Fatalf("createSession: %v", err)
	}

	session, ok := a.sessionFromRequest(requestWithSessionCookie(t, a, token))
	if !ok {
		t.Fatal("expected matching-version token to be accepted")
	}
	if session.SessionVersion != 3 {
		t.Fatalf("session version = %d, want 3", session.SessionVersion)
	}
	if !session.Permissions.CanView {
		t.Fatal("expected permissions to be resolved for accepted session")
	}
}

func TestSessionFromRequestRejectsRevokedToken(t *testing.T) {
	store := &fakeSessionStore{version: 1, perms: authPermissions{CanView: true}}
	a := newRevocationAuthService(store)

	_, token, err := a.createSession(discordUser{ID: "u1", Username: "user"})
	if err != nil {
		t.Fatalf("createSession: %v", err)
	}
	req := requestWithSessionCookie(t, a, token)

	if _, ok := a.sessionFromRequest(req); !ok {
		t.Fatal("sanity: token should be valid before revocation")
	}

	if err := a.revokeSessions(context.Background(), "u1"); err != nil {
		t.Fatalf("revokeSessions: %v", err)
	}
	if store.bumpCalls != 1 || store.lastBumpID != "u1" {
		t.Fatalf("revokeSessions did not bump the stored version: calls=%d id=%q", store.bumpCalls, store.lastBumpID)
	}

	if _, ok := a.sessionFromRequest(requestWithSessionCookie(t, a, token)); ok {
		t.Fatal("expected token to be rejected after its session version was bumped")
	}
}

func TestSessionFromRequestRejectsDeletedUser(t *testing.T) {
	store := &fakeSessionStore{version: 4, perms: authPermissions{CanView: true}}
	a := newRevocationAuthService(store)

	_, token, err := a.createSession(discordUser{ID: "u1", Username: "user"})
	if err != nil {
		t.Fatalf("createSession: %v", err)
	}

	// Account deletion drops the row; the store then reports version 0.
	store.mu.Lock()
	store.version = 0
	store.mu.Unlock()

	if _, ok := a.sessionFromRequest(requestWithSessionCookie(t, a, token)); ok {
		t.Fatal("expected token for a deleted user (version 0) to be rejected")
	}
}

func TestSessionFromRequestRejectsLegacyTokenWithoutVersion(t *testing.T) {
	store := &fakeSessionStore{version: 1, perms: authPermissions{CanView: true}}
	a := newRevocationAuthService(store)

	// A token minted before the session_version claim existed carries sv = 0.
	token, err := a.issueSessionToken(authSession{UserID: "u1", ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatalf("issueSessionToken: %v", err)
	}

	if _, ok := a.sessionFromRequest(requestWithSessionCookie(t, a, token)); ok {
		t.Fatal("expected a pre-upgrade token (no session version) to be rejected once the store is version-aware")
	}
}

func TestHandleLogoutRevokesSessionVersion(t *testing.T) {
	store := &fakeSessionStore{version: 1, perms: authPermissions{CanView: true}}
	a := newRevocationAuthService(store)
	server := &Server{auth: a}

	_, token, err := a.createSession(discordUser{ID: "u1", Username: "user"})
	if err != nil {
		t.Fatalf("createSession: %v", err)
	}

	logoutReq := requestWithSessionCookie(t, a, token)
	rec := httptest.NewRecorder()
	server.handleLogout(rec, logoutReq)

	if rec.Code != http.StatusFound {
		t.Fatalf("logout status = %d, want %d", rec.Code, http.StatusFound)
	}
	if store.bumpCalls != 1 || store.lastBumpID != "u1" {
		t.Fatalf("logout did not revoke sessions: calls=%d id=%q", store.bumpCalls, store.lastBumpID)
	}

	if _, ok := a.sessionFromRequest(requestWithSessionCookie(t, a, token)); ok {
		t.Fatal("expected the logged-out token to stop validating")
	}
}

func TestHandleLogoutWithoutSessionDoesNotRevoke(t *testing.T) {
	store := &fakeSessionStore{version: 1, perms: authPermissions{CanView: true}}
	server := &Server{auth: newRevocationAuthService(store)}

	rec := httptest.NewRecorder()
	server.handleLogout(rec, httptest.NewRequest(http.MethodGet, "http://localhost/auth/logout", nil))

	if rec.Code != http.StatusFound {
		t.Fatalf("logout status = %d, want %d", rec.Code, http.StatusFound)
	}
	if store.bumpCalls != 0 {
		t.Fatalf("logout without a session bumped the version %d time(s)", store.bumpCalls)
	}
}
