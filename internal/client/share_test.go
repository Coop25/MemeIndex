package client

import (
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"memeindex/internal/accessor"
	"memeindex/internal/manager"
)

func newShareTestServer(t *testing.T) (*Server, *accessor.MemeStore, accessor.Meme) {
	t.Helper()
	store, err := accessor.NewMemeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	meme, err := store.Create(accessor.CreateInput{
		File:        strings.NewReader("test image bytes"),
		Header:      textproto.MIMEHeader{"Content-Type": []string{"image/png"}},
		Filename:    "shared.png",
		ContentType: "image/png",
	})
	if err != nil {
		t.Fatal(err)
	}
	return &Server{managers: manager.NewMemeManager(store), shareSecret: []byte("test-share-secret")}, store, meme
}

func TestExpiredTokenFallsBackWithoutHistoricalShareRecord(t *testing.T) {
	server, store, meme := newShareTestServer(t)
	now := time.Now().UTC()
	share, err := store.GetOrCreateMemeShare(meme.ID, "admin-1", now.Add(-31*24*time.Hour), now.Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	token := server.signMemeShare(share)
	if server.validMemeShareToken(token, share, now) {
		t.Fatal("expired token validated")
	}
	active, err := store.ListActiveMemeShares(now)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 0 {
		t.Fatalf("active shares = %d, want 0", len(active))
	}
}

func TestInvalidShareFallsBackToAuthenticatedMemeRoute(t *testing.T) {
	server, _, meme := newShareTestServer(t)
	server.auth = newAuthService(DiscordAuthConfig{
		ClientID: "client", ClientSecret: "secret", RedirectURL: "http://localhost/auth/callback",
		SessionSecret: "session-secret", SessionDuration: time.Hour,
	}, nil)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "http://localhost/m/"+meme.ID+"?share=expired", nil)
	server.handleMemeLink(recorder, request)
	if recorder.Code != http.StatusFound {
		t.Fatalf("status = %d", recorder.Code)
	}
	if location := recorder.Header().Get("Location"); location != "/m/"+meme.ID {
		t.Fatalf("dead-token redirect = %q", location)
	}
	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "http://localhost/m/"+meme.ID+"/media/shared.png?share=expired", nil)
	server.handleMemeLink(recorder, request)
	if recorder.Code != http.StatusFound || recorder.Header().Get("Location") != "/m/"+meme.ID {
		t.Fatalf("dead media redirect status = %d, location = %q", recorder.Code, recorder.Header().Get("Location"))
	}
	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "http://localhost/m/"+meme.ID, nil)
	server.handleMemeLink(recorder, request)
	if recorder.Code != http.StatusFound || !strings.HasPrefix(recorder.Header().Get("Location"), "/auth/login?return_to=") {
		t.Fatalf("login redirect = %q", recorder.Header().Get("Location"))
	}
}

func TestAdminCanListAndRevokeActiveShare(t *testing.T) {
	server, store, meme := newShareTestServer(t)
	now := time.Now().UTC()
	if _, err := store.GetOrCreateMemeShare(meme.ID, "admin-1", now, now.Add(memeShareLifetime)); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	server.handleAdminShares(recorder, httptest.NewRequest(http.MethodGet, "https://memes.example.com/api/admin/shares", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), meme.ID) || !strings.Contains(recorder.Body.String(), `"url":"https://memes.example.com/s/`) {
		t.Fatalf("list status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	recorder = httptest.NewRecorder()
	server.handleAdminShares(recorder, httptest.NewRequest(http.MethodDelete, "https://memes.example.com/api/admin/shares/"+meme.ID, nil))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("revoke status = %d", recorder.Code)
	}
	active, err := store.ListActiveMemeShares(time.Now().UTC())
	if err != nil || len(active) != 0 {
		t.Fatalf("active shares after revoke = %d, err = %v", len(active), err)
	}
}

func TestAdminCanRevokeAllActiveShares(t *testing.T) {
	server, store, first := newShareTestServer(t)
	second, err := store.Create(accessor.CreateInput{
		File:        strings.NewReader("second image"),
		Header:      textproto.MIMEHeader{"Content-Type": []string{"image/png"}},
		Filename:    "second.png",
		ContentType: "image/png",
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, memeID := range []string{first.ID, second.ID} {
		if _, err := store.GetOrCreateMemeShare(memeID, "admin-1", now, now.Add(memeShareLifetime)); err != nil {
			t.Fatal(err)
		}
	}
	recorder := httptest.NewRecorder()
	server.handleAdminShares(recorder, httptest.NewRequest(http.MethodDelete, "https://memes.example.com/api/admin/shares", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"revoked":2`) {
		t.Fatalf("bulk revoke status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	active, err := store.ListActiveMemeShares(time.Now().UTC())
	if err != nil || len(active) != 0 {
		t.Fatalf("active shares after bulk revoke = %d, err = %v", len(active), err)
	}
}

func TestAdminDashboardIncludesActiveShareCount(t *testing.T) {
	server, store, meme := newShareTestServer(t)
	now := time.Now().UTC()
	if _, err := store.GetOrCreateMemeShare(meme.ID, "admin-1", now, now.Add(memeShareLifetime)); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	server.handleAdminDashboard(recorder, httptest.NewRequest(http.MethodGet, "https://memes.example.com/api/admin/dashboard", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"active_share_count":1`) {
		t.Fatalf("dashboard status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestSafeLocalReturnPathRejectsExternalTargets(t *testing.T) {
	for _, unsafe := range []string{"https://evil.example", "//evil.example/path", "javascript:alert(1)", "relative"} {
		if got := safeLocalReturnPath(unsafe); got != "" {
			t.Errorf("safeLocalReturnPath(%q) = %q", unsafe, got)
		}
	}
	if got := safeLocalReturnPath("/m/meme-123?share=dead"); got != "/m/meme-123?share=dead" {
		t.Fatalf("safe return path = %q", got)
	}
}
