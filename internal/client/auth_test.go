package client

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"memeindex/internal/accessor"
)

func TestAuthorizeAssetRequestRequiresViewPermissionAndProtectedPath(t *testing.T) {
	authority := newAuthService(DiscordAuthConfig{
		ClientID:        "client",
		ClientSecret:    "secret",
		RedirectURL:     "http://localhost/auth/callback",
		SessionSecret:   "test-session-secret",
		SessionDuration: 24 * time.Hour,
	}, nil)
	session := authSession{
		UserID: "user-123",
		Permissions: authPermissions{
			CanView: true,
		},
		ExpiresAt: time.Now().Add(2 * time.Hour),
	}

	request := httptest.NewRequest("GET", "/uploads/demo.png", nil)
	if !authority.authorizeAssetRequest(session, request) {
		t.Fatal("expected protected asset request to be accepted for viewer session")
	}

	unprivileged := authSession{
		UserID:      "user-123",
		Permissions: authPermissions{},
		ExpiresAt:   time.Now().Add(2 * time.Hour),
	}
	if authority.authorizeAssetRequest(unprivileged, request) {
		t.Fatal("expected unprivileged asset request to be rejected")
	}

	nonAssetRequest := httptest.NewRequest("GET", "/api/memes", nil)
	if authority.authorizeAssetRequest(session, nonAssetRequest) {
		t.Fatal("expected non-asset request to be rejected")
	}
}

func TestProtectMemeForResponseLeavesAssetPathsUnchanged(t *testing.T) {
	server := &Server{
		auth: newAuthService(DiscordAuthConfig{
			ClientID:        "client",
			ClientSecret:    "secret",
			RedirectURL:     "http://localhost/auth/callback",
			SessionSecret:   "test-session-secret",
			SessionDuration: 24 * time.Hour,
			SuperAdminUserIDs: map[string]struct{}{
				"user-123": {},
			},
		}, nil),
	}
	session := authSession{
		UserID: "user-123",
		Permissions: authPermissions{
			CanView: true,
		},
		ExpiresAt: time.Now().Add(2 * time.Hour),
	}
	request := httptest.NewRequest("GET", "/api/memes", nil).WithContext(contextWithSession(httptest.NewRequest("GET", "/api/memes", nil).Context(), session))

	meme := accessor.Meme{
		FilePath:    "/uploads/demo.png",
		PreviewPath: "/thumbnails/demo.webp",
	}
	protected := server.protectMemeForResponse(request, meme)

	if protected.FilePath != meme.FilePath {
		t.Fatalf("file path = %q, want %q", protected.FilePath, meme.FilePath)
	}
	if protected.PreviewPath != meme.PreviewPath {
		t.Fatalf("preview path = %q, want %q", protected.PreviewPath, meme.PreviewPath)
	}
}

func TestWithProtectedAssetAuthRejectsAnonymousRequestsAndDisablesCaching(t *testing.T) {
	server := &Server{
		auth: newAuthService(DiscordAuthConfig{
			ClientID:        "client",
			ClientSecret:    "secret",
			RedirectURL:     "http://localhost/auth/callback",
			SessionSecret:   "test-session-secret",
			SessionDuration: 24 * time.Hour,
			SuperAdminUserIDs: map[string]struct{}{
				"user-123": {},
			},
		}, nil),
	}

	handler := server.withProtectedAssetAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/uploads/demo.png", nil)
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "private, no-store, max-age=0" {
		t.Fatalf("Cache-Control = %q", got)
	}
	if got := recorder.Header().Get("CDN-Cache-Control"); got != "private, no-store, max-age=0" {
		t.Fatalf("CDN-Cache-Control = %q", got)
	}
	if got := recorder.Header().Get("Cloudflare-CDN-Cache-Control"); got != "private, no-store, max-age=0" {
		t.Fatalf("Cloudflare-CDN-Cache-Control = %q", got)
	}
	if got := recorder.Header().Get("Surrogate-Control"); got != "no-store" {
		t.Fatalf("Surrogate-Control = %q", got)
	}
	if got := recorder.Header().Get("Vary"); got != "Cookie" {
		t.Fatalf("Vary = %q", got)
	}
}

func TestWithProtectedAssetAuthAcceptsLoggedInRequestsWithoutAssetToken(t *testing.T) {
	server := &Server{
		auth: newAuthService(DiscordAuthConfig{
			ClientID:        "client",
			ClientSecret:    "secret",
			RedirectURL:     "http://localhost/auth/callback",
			SessionSecret:   "test-session-secret",
			SessionDuration: 24 * time.Hour,
			SuperAdminUserIDs: map[string]struct{}{
				"user-123": {},
			},
		}, nil),
	}

	handler := server.withProtectedAssetAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	session := authSession{
		UserID: "user-123",
		Permissions: authPermissions{
			CanView: true,
		},
		ExpiresAt: time.Now().Add(2 * time.Hour),
	}
	token, err := server.auth.issueSessionToken(session)
	if err != nil {
		t.Fatalf("issueSessionToken returned error: %v", err)
	}

	cookieRecorder := httptest.NewRecorder()
	cookieRequest := httptest.NewRequest(http.MethodGet, "http://localhost/api/memes", nil)
	server.auth.setSessionCookie(cookieRecorder, cookieRequest, token, session.ExpiresAt)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "http://localhost/uploads/demo.png", nil)
	for _, cookie := range cookieRecorder.Result().Cookies() {
		request.AddCookie(cookie)
	}
	parsedSession, ok := server.auth.sessionFromRequest(request)
	if !ok {
		t.Fatal("expected sessionFromRequest to accept the generated session cookie")
	}
	if !parsedSession.Permissions.CanView {
		t.Fatal("expected parsed session to keep CanView permission")
	}
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
}
