package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"memeindex/internal/accessor"
	"memeindex/internal/manager"
)

func TestAudiobookUploadRejectsMarkupAndNarrowsServedType(t *testing.T) {
	for _, tc := range []struct {
		name    string
		bytes   string
		file    string
		allowed bool
		served  string
	}{
		{"html", "<!DOCTYPE html><html><script src=\"/uploads/x.js\"></script>", "book.mp3", false, ""},
		{"svg", "<?xml version=\"1.0\"?><svg xmlns=\"http://www.w3.org/2000/svg\"></svg>", "book.m4b", false, ""},
		{"plain text", "just some words, definitely not audio", "book.mp3", false, ""},
		{"png", "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR", "book.mp3", false, ""},
		{"id3 mp3", "ID3\x04\x00\x00\x00\x00\x00\x00\xff\xfb\x90\x00", "book.mp3", true, "audio/mpeg"},
		{"mp3 without id3", "\xff\xfb\x90\x64\x00\x00\x00\x00\x00\x00\x00\x00", "book.mp3", true, "audio/mpeg"},
		{"m4b", "\x00\x00\x00\x20ftypM4B \x00\x00\x00\x00M4B mp42isom", "book.m4b", true, "audio/mp4"},
		{"unknown binary", "\x00\x01\x02\x03\x04\x05\x06\x07", "book.bin", true, "application/octet-stream"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sniffed := http.DetectContentType([]byte(tc.bytes))
			if got := audiobookSniffLooksLikeMedia(sniffed); got != tc.allowed {
				t.Fatalf("audiobookSniffLooksLikeMedia(%q) = %v, want %v", sniffed, got, tc.allowed)
			}
			if !tc.allowed {
				return
			}
			if got := audiobookServableMimeType(sniffed, tc.file); got != tc.served {
				t.Fatalf("audiobookServableMimeType(%q, %q) = %q, want %q", sniffed, tc.file, got, tc.served)
			}
		})
	}
}

type singleAudiobookStore struct {
	audiobookStore
	record audiobookRecord
}

func (s singleAudiobookStore) GetAudiobook(_ context.Context, id string) (audiobookRecord, error) {
	if id != s.record.ID {
		return audiobookRecord{}, ErrAudiobookNotFound
	}
	return s.record, nil
}

// An audiobook stored with a hostile type before uploads were validated must
// never be served as a document on this origin.
func TestAudiobookMediaNeverServesStoredMarkupType(t *testing.T) {
	path := filepath.Join(t.TempDir(), "evil.mp3")
	if err := os.WriteFile(path, []byte("<!DOCTYPE html><script src=/uploads/x.js></script>"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := &Server{audiobooks: singleAudiobookStore{record: audiobookRecord{ID: "b1", Filename: "evil.mp3", MimeType: "text/html; charset=utf-8", StoragePath: path}}}

	rec := httptest.NewRecorder()
	server.handleAudiobookMedia(rec, httptest.NewRequest(http.MethodGet, "/audiobook-media/b1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/octet-stream" {
		t.Fatalf("Content-Type = %q, want application/octet-stream", got)
	}
	if got := rec.Header().Get("Content-Security-Policy"); !strings.HasPrefix(got, "sandbox") {
		t.Fatalf("Content-Security-Policy = %q, want a sandbox policy", got)
	}

	// A real audio type is still served as-is so the player works.
	server.audiobooks = singleAudiobookStore{record: audiobookRecord{ID: "b1", Filename: "ok.mp3", MimeType: "audio/mpeg", StoragePath: path}}
	rec = httptest.NewRecorder()
	server.handleAudiobookMedia(rec, httptest.NewRequest(http.MethodGet, "/audiobook-media/b1", nil))
	if got := rec.Header().Get("Content-Type"); got != "audio/mpeg" {
		t.Fatalf("Content-Type = %q, want audio/mpeg", got)
	}
}

// Uploaded scripts and stylesheets must not be loadable as same-origin code.
func TestUntrustedAssetHeadersNeutralizeScriptTypes(t *testing.T) {
	for _, tc := range []struct {
		path, preset, want string
	}{
		{"/uploads/a.js", "", "application/octet-stream"},
		{"/uploads/a.mjs", "", "application/octet-stream"},
		{"/uploads/a.css", "", "application/octet-stream"},
		{"/uploads/a.html", "", "application/octet-stream"},
		{"/uploads/a.svg", "", ""},                                        // left to the file server, so <img> previews render
		{"/uploads/a.png", "text/javascript", "application/octet-stream"}, // a client-claimed type on a share link
		{"/uploads/a.png", "image/png", "image/png"},
	} {
		rec := httptest.NewRecorder()
		if tc.preset != "" {
			rec.Header().Set("Content-Type", tc.preset)
		}
		setUntrustedAssetHeaders(rec, tc.path)
		if got := rec.Header().Get("Content-Type"); got != tc.want {
			t.Errorf("%s (preset %q): Content-Type = %q, want %q", tc.path, tc.preset, got, tc.want)
		}
	}
}

func newPatchTestServer(t *testing.T) (*Server, accessor.Meme) {
	t.Helper()
	server, _ := newShareTargetTestServer(t)
	meme, err := server.managers.CreateMemeAs(accessor.AuditActor{}, strings.NewReader("hello"), textproto.MIMEHeader{}, "a.txt", []string{"cat", "dog"}, "original notes")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.managers.SetFavorite("", meme.ID, true); err != nil {
		t.Fatal(err)
	}
	return server, meme
}

func patchMeme(t *testing.T, server *Server, id, body string) (int, string) {
	t.Helper()
	handler := server.withAPIAuth(http.HandlerFunc(server.handleMemeByID), permissionView)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPatch, "/api/memes/"+id, strings.NewReader(body)))
	return rec.Code, rec.Body.String()
}

// With MEMEINDEX_ALLOW_ANONYMOUS there is no session at all; editing used to
// be refused outright.
func TestAnonymousModeCanEditMemes(t *testing.T) {
	server, meme := newPatchTestServer(t)
	if code, body := patchMeme(t, server, meme.ID, `{"tags":["cat","dog","bird"],"notes":"hi"}`); code != http.StatusOK {
		t.Fatalf("anonymous PATCH status = %d: %s", code, body)
	}
	got, err := server.managers.GetMeme("", meme.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Notes != "hi" || len(got.Tags) != 3 {
		t.Fatalf("meme after PATCH = notes %q tags %v", got.Notes, got.Tags)
	}
}

// A PATCH that leaves fields out must leave them alone.
func TestMemePatchKeepsOmittedFields(t *testing.T) {
	server, meme := newPatchTestServer(t)
	if code, body := patchMeme(t, server, meme.ID, `{"notes":"only the notes changed"}`); code != http.StatusOK {
		t.Fatalf("PATCH status = %d: %s", code, body)
	}
	got, err := server.managers.GetMeme("", meme.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Notes != "only the notes changed" {
		t.Fatalf("notes = %q", got.Notes)
	}
	if strings.Join(got.Tags, ",") != "cat,dog" {
		t.Fatalf("tags were changed by a notes-only PATCH: %v", got.Tags)
	}
	if !got.Favorite {
		t.Fatal("a notes-only PATCH unfavorited the meme")
	}
}

// Tag deltas merge with whatever is on the meme now, so a concurrent edit by
// someone else is not overwritten.
func TestMemePatchTagDeltaMergesWithCurrentTags(t *testing.T) {
	server, meme := newPatchTestServer(t)
	// Someone else adds "fish" after this client loaded the meme.
	if code, body := patchMeme(t, server, meme.ID, `{"tags_added":["fish"]}`); code != http.StatusOK {
		t.Fatalf("first PATCH status = %d: %s", code, body)
	}
	// This client, which never saw "fish", adds "bird" and removes "dog".
	if code, body := patchMeme(t, server, meme.ID, `{"tags_added":["bird"],"tags_removed":["dog"]}`); code != http.StatusOK {
		t.Fatalf("second PATCH status = %d: %s", code, body)
	}
	got, err := server.managers.GetMeme("", meme.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got.Tags, ",") != "bird,cat,fish" {
		t.Fatalf("tags = %v, want [bird cat fish]", got.Tags)
	}
}

func TestMemePatchTagDeltaStillEnforcesTagPermissions(t *testing.T) {
	server, meme := newPatchTestServer(t)
	server.auth = &authService{}
	handler := http.HandlerFunc(server.handleMemeByID)
	req := httptest.NewRequest(http.MethodPatch, "/api/memes/"+meme.ID, strings.NewReader(`{"tags_removed":["dog"]}`))
	req = req.WithContext(contextWithSession(req.Context(), authSession{UserID: "u1", Permissions: authPermissions{CanView: true, CanAddTags: true}}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("removing a tag without remove permission: status = %d, want 403", rec.Code)
	}
}

func TestClientIPTrustsCloudflareHeaderOnlyFromPrivatePeers(t *testing.T) {
	server := &Server{}
	request := func(remote string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = remote
		r.Header.Set("CF-Connecting-IP", "198.51.100.7")
		return r
	}
	if got := server.clientIP(request("172.18.0.5:41234")); got != "198.51.100.7" {
		t.Errorf("cloudflared on the docker network: clientIP = %q, want the visitor's address", got)
	}
	if got := server.clientIP(request("203.0.113.9:41234")); got != "203.0.113.9" {
		t.Errorf("public peer spoofing the header: clientIP = %q, want the peer", got)
	}
	local := request("192.168.1.20:41234")
	local = local.WithContext(context.WithValue(local.Context(), localTrustedContextKey{}, true))
	if got := server.clientIP(local); got != "192.168.1.20" {
		t.Errorf("local listener: clientIP = %q, want the direct peer", got)
	}
	junk := request("172.18.0.5:41234")
	junk.Header.Set("CF-Connecting-IP", "not-an-ip")
	if got := server.clientIP(junk); got != "172.18.0.5" {
		t.Errorf("malformed header: clientIP = %q, want the peer", got)
	}
}

func TestSessionCookieSecureBehindHTTPSProxy(t *testing.T) {
	a := &authService{}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if a.cookieSecureForRequest(r) {
		t.Fatal("plain HTTP with no proxy header should not mark cookies Secure")
	}
	r.Header.Set("X-Forwarded-Proto", "https")
	if !a.cookieSecureForRequest(r) {
		t.Fatal("a request the proxy received over HTTPS should get Secure cookies")
	}
}

type hidingMemeStore struct {
	*accessor.MemeStore
	hidden []string
}

func (s hidingMemeStore) HiddenMemeIDs() ([]string, error) { return s.hidden, nil }

func TestProtectedAssetsHideDirectoryListingsAndPendingDeletes(t *testing.T) {
	dir := t.TempDir()
	store, err := accessor.NewMemeStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	files := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	fetch := func(perms authPermissions, path string) int {
		t.Helper()
		auth := newRevocationAuthService(&fakeSessionStore{version: 1, perms: perms})
		server := &Server{managers: manager.NewMemeManager(hidingMemeStore{MemeStore: store, hidden: []string{"hidden-id"}}), auth: auth}
		_, token, err := auth.createSession(discordUser{ID: "u1", Username: "user"})
		if err != nil {
			t.Fatal(err)
		}
		req := requestWithSessionCookie(t, auth, token)
		req.URL.Path = path
		rec := httptest.NewRecorder()
		server.withProtectedAssetAuth(files).ServeHTTP(rec, req)
		return rec.Code
	}

	viewer := authPermissions{CanView: true}
	admin := authPermissions{CanView: true, CanManageUsers: true}
	for _, tc := range []struct {
		name  string
		perms authPermissions
		path  string
		want  int
	}{
		{"directory listing", viewer, "/uploads/", http.StatusNotFound},
		{"thumbnail directory listing", admin, "/thumbnails/", http.StatusNotFound},
		{"visible upload", viewer, "/uploads/visible-id.png", http.StatusOK},
		{"pending-delete upload", viewer, "/uploads/hidden-id.png", http.StatusNotFound},
		{"pending-delete thumbnail", viewer, "/thumbnails/hidden-id.jpg", http.StatusNotFound},
		{"admin reviewing the delete queue", admin, "/uploads/hidden-id.png", http.StatusOK},
	} {
		if got := fetch(tc.perms, tc.path); got != tc.want {
			t.Errorf("%s (%s): status = %d, want %d", tc.name, tc.path, got, tc.want)
		}
	}
}

func TestClientSafeErrorHidesInternalDetail(t *testing.T) {
	if got := clientSafeError(errors.New("yt-dlp: ERROR: [youtube] /app/data/downloads/x: HTTP 403"), "could not download that link"); got != "could not download that link" {
		t.Fatalf("internal error leaked: %q", got)
	}
	if got := clientSafeError(errUnsupportedMediaURL, "fallback"); got != errUnsupportedMediaURL.Error() {
		t.Fatalf("friendly error replaced: %q", got)
	}
	if !errors.Is(errUnsupportedMediaURL, errUnsupportedMediaURL) {
		t.Fatal("sentinel identity broken")
	}
}
