package client

import (
	"bytes"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"net/url"
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

func TestCreateMemeShareReturnsStableSignedPageURL(t *testing.T) {
	server, _, meme := newShareTestServer(t)
	create := func() string {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "http://internal/api/memes/"+meme.ID+"/share", nil)
		request.Header.Set("X-Forwarded-Proto", "https")
		request.Header.Set("X-Forwarded-Host", "memes.example.com")
		server.createMemeShare(recorder, request, meme.ID)
		if recorder.Code != http.StatusCreated {
			t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
		}
		body := recorder.Body.String()
		start := strings.Index(body, `"url":"`) + len(`"url":"`)
		end := strings.Index(body[start:], `"`)
		return body[start : start+end]
	}
	first := create()
	second := create()
	if first != second {
		t.Fatalf("active share URL changed: %q != %q", first, second)
	}
	parsed, err := url.Parse(first)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Path != "/m/"+meme.ID || parsed.Query().Get("share") == "" {
		t.Fatalf("share URL = %q", first)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "https://memes.example.com/api/memes/"+meme.ID+"/share", nil)
	server.createMemeShare(recorder, request, meme.ID)
	body := recorder.Body.String()
	if !strings.Contains(body, `"page_url":"https://memes.example.com/m/`+meme.ID+`?share=`) {
		t.Fatalf("share response is missing page URL: %s", body)
	}
	if !strings.Contains(body, `"media_url":"https://memes.example.com/m/`+meme.ID+`/media/shared.png?share=`) {
		t.Fatalf("share response is missing media URL: %s", body)
	}
}

func TestSignedShareEmbedsAndRevocationDeniesPublicMedia(t *testing.T) {
	server, store, meme := newShareTestServer(t)
	now := time.Now().UTC()
	share, err := store.GetOrCreateMemeShare(meme.ID, "admin-1", now, now.Add(memeShareLifetime))
	if err != nil {
		t.Fatal(err)
	}
	token := server.signMemeShare(share)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "https://memes.example.com/m/"+meme.ID+"?share="+url.QueryEscape(token), nil)
	server.handleMemeLink(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("active status = %d", recorder.Code)
	}
	if body := recorder.Body.String(); !strings.Contains(body, `/m/`+meme.ID+`/media/shared.png?share=`) || !strings.Contains(body, `property="og:image:secure_url"`) || !strings.Contains(body, `name="twitter:image"`) {
		t.Fatalf("active share is missing Discord metadata: %s", body)
	}

	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "https://memes.example.com/m/"+meme.ID+"/media/shared.png?share="+url.QueryEscape(token), nil)
	server.handleMemeLink(recorder, request)
	if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Type") != "image/png" || recorder.Header().Get("Accept-Ranges") != "bytes" {
		t.Fatalf("shared asset status = %d, content-type = %q, accept-ranges = %q", recorder.Code, recorder.Header().Get("Content-Type"), recorder.Header().Get("Accept-Ranges"))
	}

	if err := store.RevokeMemeShare(meme.ID); err != nil {
		t.Fatal(err)
	}
	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "https://memes.example.com/m/"+meme.ID+"/media?share="+url.QueryEscape(token), nil)
	server.handleMemeLink(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("revoked media status = %d", recorder.Code)
	}
}

// Discord unfurls a link with Discordbot but its media proxy re-fetches the
// embed later under a different User-Agent. Serving raw media only to
// Discordbot meant the proxy got HTML instead of the image, so embeds broke
// intermittently. Every client must now get the same page, whose OpenGraph
// tags point at the UA-independent /media/ URL.
func TestShareLinkServesSamePageToEveryUserAgent(t *testing.T) {
	server, store, meme := newShareTestServer(t)
	now := time.Now().UTC()
	share, err := store.GetOrCreateMemeShare(meme.ID, "admin-1", now, now.Add(memeShareLifetime))
	if err != nil {
		t.Fatal(err)
	}
	shareURL := "https://memes.example.com/m/" + meme.ID + "?share=" + url.QueryEscape(server.signMemeShare(share))

	for _, userAgent := range []string{
		"Mozilla/5.0 (compatible; Discordbot/2.0; +https://discordapp.com)",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10.11; rv:92.0) Gecko/20100101 Firefox/92.0",
		"Mozilla/5.0 Chrome/140.0.0.0 Safari/537.36",
	} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, shareURL, nil)
		request.Header.Set("User-Agent", userAgent)
		server.handleMemeLink(recorder, request)
		if recorder.Code != http.StatusOK || !strings.HasPrefix(recorder.Header().Get("Content-Type"), "text/html") {
			t.Fatalf("%s: status = %d, content-type = %q", userAgent, recorder.Code, recorder.Header().Get("Content-Type"))
		}
		body := recorder.Body.String()
		if !strings.Contains(body, `property="og:image" content="https://memes.example.com/m/`+meme.ID+`/media/shared.png?share=`) {
			t.Fatalf("%s: page is missing og:image media URL: %s", userAgent, body)
		}
		if vary := recorder.Header().Get("Vary"); strings.Contains(vary, "User-Agent") {
			t.Fatalf("%s: response still varies on User-Agent", userAgent)
		}
	}
}

func TestSharePageReportsImageDimensions(t *testing.T) {
	store, err := accessor.NewMemeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 640, 360))); err != nil {
		t.Fatal(err)
	}
	meme, err := store.Create(accessor.CreateInput{
		File:        &encoded,
		Header:      textproto.MIMEHeader{"Content-Type": []string{"image/png"}},
		Filename:    "wide.png",
		ContentType: "image/png",
	})
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{managers: manager.NewMemeManager(store), shareSecret: []byte("test-share-secret")}
	now := time.Now().UTC()
	share, err := store.GetOrCreateMemeShare(meme.ID, "admin-1", now, now.Add(memeShareLifetime))
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "https://memes.example.com/m/"+meme.ID+"?share="+url.QueryEscape(server.signMemeShare(share)), nil)
	server.handleMemeLink(recorder, request)
	body := recorder.Body.String()
	if !strings.Contains(body, `property="og:description" content="Shared privately until `) {
		t.Fatalf("share page description does not use the real expiry: %s", body)
	}
	if !strings.Contains(body, `property="og:image:width" content="640"`) || !strings.Contains(body, `property="og:image:height" content="360"`) {
		t.Fatalf("share page is missing image dimensions: %s", body)
	}
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
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), meme.ID) || !strings.Contains(recorder.Body.String(), `?share=`) {
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
