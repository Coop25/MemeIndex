package client

import (
	"bytes"
	"context"
	"errors"
	"memeindex/internal/accessor"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRemoveOnlyUserCannotApplySuggestedTag(t *testing.T) {
	s, _, meme := newShareTestServer(t)
	s.auth = newAuthService(DiscordAuthConfig{ClientID: "c", ClientSecret: "s", RedirectURL: "https://example.com/auth/callback", SessionSecret: "test"}, nil)
	r := httptest.NewRequest(http.MethodPatch, "/api/memes/"+meme.ID+"/tag-suggestions", strings.NewReader(`{"action":"add","tag":"unauthorized-tag"}`))
	r = r.WithContext(contextWithSession(r.Context(), authSession{UserID: "remove-only", Permissions: authPermissions{CanView: true, CanRemoveTags: true}}))
	w := httptest.NewRecorder()
	s.handleMemeByID(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("remove-only user added a tag: status %d", w.Code)
	}
	stored, err := s.managers.GetMeme("remove-only", meme.ID)
	if err != nil || len(stored.Tags) != 0 {
		t.Fatalf("unauthorized mutation: %+v, %v", stored.Tags, err)
	}
}

func TestSupportedSourceURLRejectsLookalikes(t *testing.T) {
	for _, raw := range []string{"https://youtube.com.attacker.example/watch", "https://notfacebook.com/", "https://attacker.example/?youtube.com", "https://youtube.com@attacker.example/", "https://youtube.com:8443/", "file://youtube.com/video"} {
		if supportedSourceURL(raw) {
			t.Errorf("accepted %s", raw)
		}
		if _, err := normalizeSourceURL(context.Background(), raw); !errors.Is(err, errUnsupportedMediaURL) {
			t.Errorf("not rejected before network I/O: %s: %v", raw, err)
		}
	}
	for _, raw := range []string{"https://www.youtube.com/watch?v=abc", "https://youtu.be/abc", "https://v.redd.it/abc", "https://vm.tiktok.com/abc"} {
		if !supportedSourceURL(raw) {
			t.Errorf("rejected supported URL %s", raw)
		}
	}
}

func TestPublicDialerPinsAddressAndRejectsMixedDNS(t *testing.T) {
	for _, ips := range [][]string{{"8.8.8.8"}, {"127.0.0.1"}, {"8.8.8.8", "10.0.0.1"}, {"::ffff:127.0.0.1"}, {"100.64.0.1"}, {"169.254.169.254"}, {"64:ff9b::7f00:1"}} {
		called := false
		d := publicRemoteDialer{
			lookup: func(context.Context, string) ([]net.IPAddr, error) {
				var addresses []net.IPAddr
				for _, ip := range ips {
					addresses = append(addresses, net.IPAddr{IP: net.ParseIP(ip)})
				}
				return addresses, nil
			},
			dial: func(_ context.Context, _, address string) (net.Conn, error) {
				called = true
				if address != "8.8.8.8:443" {
					t.Errorf("dialed unvalidated address %s", address)
				}
				return nil, errors.New("test dial: no real network connection")
			},
		}
		_, err := d.DialContext(context.Background(), "tcp", "example.com:443")
		if err == nil {
			t.Fatal("expected error from fake dialer or address rejection")
		}
		if called != (len(ips) == 1 && ips[0] == "8.8.8.8") {
			t.Errorf("unsafe dial for %v: %v", ips, called)
		}
	}
}

func TestSecurityConfigurationFailsClosed(t *testing.T) {
	good := Config{MaxUploadBytes: 1024, DiscordAuth: DiscordAuthConfig{ClientID: "c", ClientSecret: "s", RedirectURL: "https://example.com/auth/callback", SessionSecret: strings.Repeat("r", 32)}}
	if err := good.validateSecurity(); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"client", "secret", "redirect", "session"} {
		bad := good
		switch field {
		case "client":
			bad.DiscordAuth.ClientID = ""
		case "secret":
			bad.DiscordAuth.ClientSecret = ""
		case "redirect":
			bad.DiscordAuth.RedirectURL = ""
		case "session":
			bad.DiscordAuth.SessionSecret = ""
		}
		bad.AllowAnonymous = true
		if bad.validateSecurity() == nil {
			t.Errorf("partial auth (%s) failed open", field)
		}
	}
	for _, secret := range []string{"short", "replace-with-a-long-random-string", "replace-with-a-long-random-string-12345"} {
		bad := good
		bad.DiscordAuth.SessionSecret = secret
		if bad.validateSecurity() == nil {
			t.Error("accepted weak/example secret")
		}
	}
	if (Config{MaxUploadBytes: 1024}).validateSecurity() == nil {
		t.Error("implicit anonymous operation accepted")
	}
	if err := (Config{MaxUploadBytes: 1024, AllowAnonymous: true}).validateSecurity(); err != nil {
		t.Fatal(err)
	}
}

func TestOversizedUploadRejectedBeforeStorage(t *testing.T) {
	var body bytes.Buffer
	m := multipart.NewWriter(&body)
	file, err := m.CreateFormFile("file", "test.bin")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.Write(bytes.Repeat([]byte("x"), 2048))
	_ = m.Close()
	s := &Server{config: Config{MaxUploadBytes: 1024}}
	r := httptest.NewRequest(http.MethodPost, "/api/memes", &body)
	r.Header.Set("Content-Type", m.FormDataContentType())
	w := httptest.NewRecorder()
	s.createMeme(w, r)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize status %d", w.Code)
	}
}

func TestReelTokenCannotCrossUsers(t *testing.T) {
	s := &Server{auth: &authService{}, shareSecret: []byte("test secret")}
	r := httptest.NewRequest(http.MethodGet, "/api/memes/random", nil)
	a := r.WithContext(contextWithSession(r.Context(), authSession{UserID: "alice"}))
	b := r.WithContext(contextWithSession(r.Context(), authSession{UserID: "bob"}))
	token := s.reelToken(a, "session-id")
	if id, ok := s.reelID(a, token); !ok || id != "session-id" {
		t.Fatal("owner rejected")
	}
	if _, ok := s.reelID(b, token); ok {
		t.Fatal("another user accepted")
	}
	if _, ok := s.reelID(a, "session-id"); ok {
		t.Fatal("unsigned session accepted")
	}
}

func TestRevokedUploaderCannotRunQueuedDownload(t *testing.T) {
	s := &Server{auth: &authService{config: DiscordAuthConfig{}}}
	err := s.processRetriedLinkJob(context.Background(), LinkRetryJob{Actor: accessor.AuditActor{UserID: "revoked"}, SourceURL: "https://youtu.be/test"})
	if err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("retry was not denied before download: %v", err)
	}
}

func TestArchiveExpandedSizeAndEntryLimits(t *testing.T) {
	archive := makeTestArchive(t, map[string]string{"uploads/a": strings.Repeat("a", 100), "uploads/b": strings.Repeat("b", 100)})
	for _, limits := range []struct {
		bytes   int64
		entries int
	}{{150, 10}, {1000, 1}} {
		if err := extractPortableArchiveWithLimits(bytes.NewReader(archive), t.TempDir(), limits.bytes, limits.entries); err == nil || !strings.Contains(err.Error(), "limits") {
			t.Fatalf("limit not enforced: %v", err)
		}
	}
}

func TestRestoreRejectsReorderedCSVColumns(t *testing.T) {
	staging := t.TempDir()
	if err := os.MkdirAll(filepath.Join(staging, "database"), 0700); err != nil {
		t.Fatal(err)
	}
	columns := strings.Split(portableBackupTables[0].columns, ", ")
	columns[1], columns[2] = columns[2], columns[1]
	if err := os.WriteFile(filepath.Join(staging, "database", "memes.csv"), []byte(strings.Join(columns, ",")+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := validateRestoredMemes(staging); err == nil {
		t.Fatal("reordered CSV accepted")
	}
}

func TestRestoreRejectsUnsafeStoredURLs(t *testing.T) {
	for _, unsafeField := range []int{3, 8} {
		staging := t.TempDir()
		for _, dir := range []string{"database", "uploads"} {
			if err := os.MkdirAll(filepath.Join(staging, dir), 0700); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(staging, "uploads", "a.mp4"), []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
		columns := strings.Split(portableBackupTables[0].columns, ", ")
		values := make([]string, len(columns))
		values[0], values[2], values[3] = "id", "a.mp4", "/uploads/a.mp4"
		values[unsafeField] = "javascript:alert(1)"
		csv := strings.Join(columns, ",") + "\n" + strings.Join(values, ",") + "\n"
		if err := os.WriteFile(filepath.Join(staging, "database", "memes.csv"), []byte(csv), 0600); err != nil {
			t.Fatal(err)
		}
		if err := validateRestoredMemes(staging); err == nil {
			t.Fatalf("unsafe URL accepted at column %d", unsafeField)
		}
	}
}

func TestPendingLoginStatesAreBounded(t *testing.T) {
	a := &authService{pendingStates: map[string]time.Time{}}
	for i := 0; i < 4096; i++ {
		a.pendingStates[time.Unix(int64(i), 0).String()] = time.Now().Add(time.Minute)
	}
	if a.rememberState("new-state") {
		t.Fatal("unbounded pending logins")
	}
	for key := range a.pendingStates {
		a.pendingStates[key] = time.Now().Add(-time.Minute)
	}
	if !a.rememberState("new-state") || len(a.pendingStates) != 1 {
		t.Fatal("expired capacity not reclaimed")
	}
}

type securityUsers struct {
	authUserStore
	permissions authPermissions
}

func (u *securityUsers) UpsertSessionProfile(context.Context, authClaims) error { return nil }
func (u *securityUsers) GetUser(_ context.Context, id string) (managedUserRecord, bool, error) {
	return managedUserRecord{UserID: id, Permissions: u.permissions}, true, nil
}

func TestActualRoutesDenyViewerAdministrativeActions(t *testing.T) {
	s, _, meme := newShareTestServer(t)
	s.auth = newAuthService(DiscordAuthConfig{ClientID: "c", ClientSecret: "s", RedirectURL: "https://example.com/auth/callback", SessionSecret: "test"}, &securityUsers{permissions: authPermissions{CanView: true}})
	token, err := s.auth.issueSessionToken(authSession{UserID: "viewer", ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []struct{ method, path string }{
		{"GET", "/api/users"}, {"PATCH", "/api/users/admin"}, {"DELETE", "/api/users/admin"},
		{"GET", "/api/admin/dashboard"}, {"GET", "/api/admin/backup/download"}, {"POST", "/api/admin/backup/import"},
		{"POST", "/api/admin/backup/export"}, {"DELETE", "/api/admin/shares"}, {"GET", "/api/admin/audit-logs"},
		{"POST", "/api/admin/tag-suggestions/reset"}, {"POST", "/api/memes/save-link"}, {"POST", "/api/memes"},
		{"DELETE", "/api/memes/" + meme.ID}, {"PATCH", "/api/memes/" + meme.ID + "/tag-suggestions"},
	} {
		r := httptest.NewRequest(endpoint.method, "https://example.com"+endpoint.path, strings.NewReader(`{}`))
		w := httptest.NewRecorder()
		s.auth.setSessionCookie(w, r, token, time.Now().Add(time.Hour))
		r.AddCookie(w.Result().Cookies()[0])
		w = httptest.NewRecorder()
		s.Routes().ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Errorf("%s %s returned %d", endpoint.method, endpoint.path, w.Code)
		}
	}
}

func TestSameOriginShareCreationStillWorks(t *testing.T) {
	s, _, meme := newShareTestServer(t)
	s.auth = newAuthService(DiscordAuthConfig{ClientID: "c", ClientSecret: "s", RedirectURL: "https://example.com/auth/callback", SessionSecret: "test"}, &securityUsers{permissions: authPermissions{CanView: true}})
	token, err := s.auth.issueSessionToken(authSession{UserID: "viewer", ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "https://example.com/api/memes/"+meme.ID+"/share", nil)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.Header.Set("Origin", "https://example.com")
	w := httptest.NewRecorder()
	s.auth.setSessionCookie(w, r, token, time.Now().Add(time.Hour))
	r.AddCookie(w.Result().Cookies()[0])
	w = httptest.NewRecorder()
	s.Routes().ServeHTTP(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("legitimate share rejected: %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
		t.Fatal("authenticated response allows caching")
	}
}

func TestOAuthCallbackClearsTransientCookiesBeforeWritingResponse(t *testing.T) {
	s := &Server{auth: &authService{}}
	w := httptest.NewRecorder()
	s.handleOAuthCallback(w, httptest.NewRequest(http.MethodGet, "/auth/callback", nil))
	cookies := w.Result().Cookies()
	if len(cookies) != 2 {
		t.Fatalf("deletion headers were not sent: %v", cookies)
	}
	for _, cookie := range cookies {
		if cookie.MaxAge != -1 {
			t.Fatal("cookie not expired")
		}
	}
}

func TestCrossOriginBrowserCannotCreateShare(t *testing.T) {
	s, _, meme := newShareTestServer(t)
	s.auth = newAuthService(DiscordAuthConfig{ClientID: "c", ClientSecret: "s", RedirectURL: "https://memes.example.com/auth/callback", SessionSecret: "test", SuperAdminUserIDs: map[string]struct{}{"admin": {}}}, nil)
	token, err := s.auth.issueSessionToken(authSession{UserID: "admin", ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "https://memes.example.com/api/memes/"+meme.ID+"/share", nil)
	r.Header.Set("Origin", "https://other.example.com")
	r.Header.Set("Sec-Fetch-Site", "same-site")
	w := httptest.NewRecorder()
	s.auth.setSessionCookie(w, r, token, time.Now().Add(time.Hour))
	r.AddCookie(w.Result().Cookies()[0])
	w = httptest.NewRecorder()
	s.Routes().ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("cross-origin state change accepted: %d", w.Code)
	}
}

func TestSecureCookieCannotBeDowngradedByForwardedProtocol(t *testing.T) {
	a := &authService{config: DiscordAuthConfig{CookieSecure: true}}
	r := httptest.NewRequest(http.MethodGet, "https://memes.example.com/auth/login", nil)
	r.Header.Set("X-Forwarded-Proto", "http")
	if !a.cookieSecureForRequest(r) {
		t.Fatal("secure cookie downgraded")
	}
}

func TestReturnPathRejectsBrowserBackslashRedirect(t *testing.T) {
	for _, path := range []string{`/\evil.example`, `/%5cevil.example`, "//evil.example"} {
		if got := safeLocalReturnPath(path); got != "" {
			t.Errorf("unsafe path %q accepted as %q", path, got)
		}
	}
}
