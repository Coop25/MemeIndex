package client

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"memeindex/internal/accessor"
)

func TestAuthorizeAssetRequestRequiresSignedURL(t *testing.T) {
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

	unsignedRequest := httptest.NewRequest("GET", "/uploads/demo.png", nil)
	if authority.authorizeAssetRequest(session, unsignedRequest) {
		t.Fatal("expected unsigned asset request to be rejected")
	}

	signedURL := authority.signedAssetURL(session, "/uploads/demo.png")
	signedRequest := httptest.NewRequest("GET", signedURL, nil)
	if !authority.authorizeAssetRequest(session, signedRequest) {
		t.Fatal("expected signed asset request to be accepted")
	}
}

func TestProtectMemeForResponseSignsAssetPaths(t *testing.T) {
	server := &Server{
		auth: newAuthService(DiscordAuthConfig{
			ClientID:        "client",
			ClientSecret:    "secret",
			RedirectURL:     "http://localhost/auth/callback",
			SessionSecret:   "test-session-secret",
			SessionDuration: 24 * time.Hour,
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

	if !strings.Contains(protected.FilePath, assetTokenQueryName+"=") {
		t.Fatalf("expected file path to include %q, got %q", assetTokenQueryName, protected.FilePath)
	}
	if !strings.Contains(protected.PreviewPath, assetTokenQueryName+"=") {
		t.Fatalf("expected preview path to include %q, got %q", assetTokenQueryName, protected.PreviewPath)
	}
}
