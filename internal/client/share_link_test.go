package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

var shareFileLinkPath = regexp.MustCompile(`^/s/[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[0-9a-f]{4}-[0-9a-f]{12}\.png$`)

func getShareLink(server *Server, target string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	server.handleShareLink(recorder, httptest.NewRequest(http.MethodGet, target, nil))
	return recorder
}

func TestCreateMemeShareReturnsStableFileLink(t *testing.T) {
	server, _, meme := newShareTestServer(t)
	create := func() map[string]any {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "https://memes.example.com/api/memes/"+meme.ID+"/share", nil)
		// An untrusted forwarded host must not end up in the link.
		request.Header.Set("X-Forwarded-Host", "evil.example.com")
		server.createMemeShare(recorder, request, meme.ID)
		if recorder.Code != http.StatusCreated {
			t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
		}
		var payload map[string]any
		if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		return payload
	}
	first := create()
	second := create()
	link, _ := first["url"].(string)
	if link != second["url"] {
		t.Fatalf("active share link changed: %q != %q", link, second["url"])
	}
	parsed, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Host != "memes.example.com" || !shareFileLinkPath.MatchString(parsed.Path) || parsed.RawQuery != "" {
		t.Fatalf("share link = %q, want a bare /s/<uuid>.png link", link)
	}
	if first["page_url"] != link || first["media_url"] != link {
		t.Fatalf("page_url and media_url must match url: %v", first)
	}
	expires, err := time.Parse(time.RFC3339Nano, first["expires_at"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if left := time.Until(expires); left < 364*24*time.Hour || left > 366*24*time.Hour {
		t.Fatalf("share lasts %v, want about a year", left)
	}
}

// Discord unfurls a link with Discordbot but its media proxy re-fetches it
// later under a different User-Agent. Every client must get the same file.
func TestShareLinkServesFileToEveryUserAgent(t *testing.T) {
	server, store, meme := newShareTestServer(t)
	now := time.Now().UTC()
	share, err := store.GetOrCreateMemeShare(meme.ID, "admin-1", now, now.Add(memeShareLifetime))
	if err != nil {
		t.Fatal(err)
	}
	if share.Slug == "" {
		t.Fatal("new share has no link code")
	}
	for _, userAgent := range []string{
		"Mozilla/5.0 (compatible; Discordbot/2.0; +https://discordapp.com)",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10.11; rv:92.0) Gecko/20100101 Firefox/92.0",
		"Mozilla/5.0 Chrome/140.0.0.0 Safari/537.36",
	} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "https://memes.example.com/s/"+share.Slug+".png", nil)
		request.Header.Set("User-Agent", userAgent)
		server.handleShareLink(recorder, request)
		if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Type") != "image/png" || recorder.Body.String() != "test image bytes" {
			t.Fatalf("%s: status = %d, content-type = %q, body = %q", userAgent, recorder.Code, recorder.Header().Get("Content-Type"), recorder.Body.String())
		}
		if recorder.Header().Get("Accept-Ranges") != "bytes" {
			t.Fatalf("%s: shared file does not advertise range support", userAgent)
		}
		if got := recorder.Header().Get("Cache-Control"); got != "no-store, max-age=0" {
			t.Fatalf("%s: Cache-Control = %q", userAgent, got)
		}
	}

	// The extension is cosmetic: the code alone identifies the share.
	if recorder := getShareLink(server, "https://memes.example.com/s/"+share.Slug); recorder.Code != http.StatusOK {
		t.Fatalf("link without extension status = %d", recorder.Code)
	}
	for _, target := range []string{
		"https://memes.example.com/s/00000000-0000-4000-8000-000000000000.png",
		"https://memes.example.com/s/" + meme.ID + ".png",
		"https://memes.example.com/s/" + share.Slug + "/extra.png",
		"https://memes.example.com/s/",
	} {
		if recorder := getShareLink(server, target); recorder.Code != http.StatusNotFound {
			t.Fatalf("%s: status = %d, want 404", target, recorder.Code)
		}
	}
	recorder := httptest.NewRecorder()
	server.handleShareLink(recorder, httptest.NewRequest(http.MethodPost, "https://memes.example.com/s/"+share.Slug+".png", nil))
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d", recorder.Code)
	}
}

// Cloudflare may keep the file at its edge for a day, so a revoked or deleted
// meme can go on loading from Cloudflare for up to that long. Only the exact
// link the app hands out is cacheable; browsers and every other cache are
// still told not to store it.
func TestShareLinkLetsCloudflareCacheTheOfficialLinkForADay(t *testing.T) {
	server, store, meme := newShareTestServer(t)
	now := time.Now().UTC()
	share, err := store.GetOrCreateMemeShare(meme.ID, "admin-1", now, now.Add(memeShareLifetime))
	if err != nil {
		t.Fatal(err)
	}
	official := "https://memes.example.com/s/" + share.Slug + ".png"

	recorder := getShareLink(server, official)
	if recorder.Code != http.StatusOK {
		t.Fatalf("official link status = %d", recorder.Code)
	}
	if got := recorder.Header().Get("Cloudflare-CDN-Cache-Control"); got != "public, max-age=86400" {
		t.Fatalf("Cloudflare-CDN-Cache-Control = %q, want a day at the edge", got)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store, max-age=0" {
		t.Fatalf("Cache-Control = %q, browsers must not keep the file", got)
	}
	// Cloudflare ignores the other cache headers when Surrogate-Control is
	// present, and the generic CDN header is passed on to other caches.
	for _, name := range []string{"Surrogate-Control", "CDN-Cache-Control"} {
		if got := recorder.Header().Get(name); got != "" {
			t.Fatalf("%s = %q on a cacheable response", name, got)
		}
	}

	assertNotEdgeCached := func(label string, recorder *httptest.ResponseRecorder) {
		t.Helper()
		if got := recorder.Header().Get("Cloudflare-CDN-Cache-Control"); got != "no-store" {
			t.Fatalf("%s: Cloudflare-CDN-Cache-Control = %q, want no-store", label, got)
		}
	}
	// Other spellings of the same link would each be a separate cache entry.
	for _, target := range []string{
		"https://memes.example.com/s/" + share.Slug,
		"https://memes.example.com/s/" + share.Slug + ".gif",
		"https://memes.example.com/s/" + share.Slug + ".PNG",
		official + "?v=2",
	} {
		recorder := getShareLink(server, target)
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s: status = %d", target, recorder.Code)
		}
		assertNotEdgeCached(target, recorder)
	}
	assertNotEdgeCached("unknown code", getShareLink(server, "https://memes.example.com/s/00000000-0000-4000-8000-000000000000.png"))

	legacy := httptest.NewRecorder()
	server.handleMemeLink(legacy, httptest.NewRequest(http.MethodGet, "https://memes.example.com/m/"+meme.ID+"?share="+url.QueryEscape(server.signMemeShare(share)), nil))
	if legacy.Code != http.StatusOK {
		t.Fatalf("legacy link status = %d", legacy.Code)
	}
	assertNotEdgeCached("legacy signed link", legacy)

	if err := store.RevokeMemeShare(meme.ID); err != nil {
		t.Fatal(err)
	}
	revoked := getShareLink(server, official)
	if revoked.Code != http.StatusFound {
		t.Fatalf("revoked link status = %d", revoked.Code)
	}
	assertNotEdgeCached("revoked link redirect", revoked)
}

// A share close to its expiry must not outlive it at the edge.
func TestShareLinkEdgeCacheNeverOutlivesTheShare(t *testing.T) {
	server, store, meme := newShareTestServer(t)
	now := time.Now().UTC()
	share, err := store.GetOrCreateMemeShare(meme.ID, "admin-1", now, now.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	recorder := getShareLink(server, "https://memes.example.com/s/"+share.Slug+".png")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	header := recorder.Header().Get("Cloudflare-CDN-Cache-Control")
	maxAge, found := strings.CutPrefix(header, "public, max-age=")
	if !found {
		t.Fatalf("Cloudflare-CDN-Cache-Control = %q", header)
	}
	seconds, err := strconv.Atoi(maxAge)
	if err != nil || seconds <= 0 || seconds > 2*60*60 || seconds < 2*60*60-60 {
		t.Fatalf("max-age = %q, want about two hours and never past the share's expiry", maxAge)
	}
}

// An error from the file server, such as the file being gone from disk, must
// not be cached at the edge for a day.
func TestShareLinkErrorIsNotEdgeCached(t *testing.T) {
	server, store, meme := newShareTestServer(t)
	now := time.Now().UTC()
	share, err := store.GetOrCreateMemeShare(meme.ID, "admin-1", now, now.Add(memeShareLifetime))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(server.managers.UploadDir(), meme.StoredName)); err != nil {
		t.Fatal(err)
	}
	recorder := getShareLink(server, "https://memes.example.com/s/"+share.Slug+".png")
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for a missing file", recorder.Code)
	}
	if got := recorder.Header().Get("Cloudflare-CDN-Cache-Control"); got != "no-store" {
		t.Fatalf("Cloudflare-CDN-Cache-Control = %q on an error", got)
	}
}

func TestDeadShareLinkRedirectsToMemeAndNeverRevives(t *testing.T) {
	server, store, meme := newShareTestServer(t)
	now := time.Now().UTC()
	assertRedirectsToMeme := func(label, slug string) {
		t.Helper()
		recorder := getShareLink(server, "https://memes.example.com/s/"+slug+".png")
		if recorder.Code != http.StatusFound || recorder.Header().Get("Location") != "/m/"+meme.ID {
			t.Fatalf("%s: status = %d, location = %q", label, recorder.Code, recorder.Header().Get("Location"))
		}
		if recorder.Body.String() == "test image bytes" {
			t.Fatalf("%s: dead link served the file", label)
		}
	}

	// A share that has run past its expiry.
	expired, err := store.GetOrCreateMemeShare(meme.ID, "admin-1", now.Add(-2*time.Hour), now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	assertRedirectsToMeme("expired", expired.Slug)

	// Sharing again issues a new link; the old one stays dead.
	second, err := store.GetOrCreateMemeShare(meme.ID, "admin-1", now, now.Add(memeShareLifetime))
	if err != nil {
		t.Fatal(err)
	}
	if second.Slug == "" || second.Slug == expired.Slug {
		t.Fatalf("re-share reused the dead link code %q", second.Slug)
	}
	if recorder := getShareLink(server, "https://memes.example.com/s/"+second.Slug+".png"); recorder.Code != http.StatusOK {
		t.Fatalf("new link status = %d", recorder.Code)
	}
	assertRedirectsToMeme("expired after re-share", expired.Slug)

	// Renewing an active share keeps its link and moves the expiry.
	renewed, err := store.GetOrCreateMemeShare(meme.ID, "someone-else", now.Add(time.Minute), now.Add(2*memeShareLifetime))
	if err != nil {
		t.Fatal(err)
	}
	if renewed.Slug != second.Slug || renewed.Generation != second.Generation || !renewed.ExpiresAt.After(second.ExpiresAt) {
		t.Fatalf("renewal changed the link or did not extend it: %+v -> %+v", second, renewed)
	}
	if renewed.SharedByUserID != "admin-1" {
		t.Fatalf("renewal reassigned the share to %q", renewed.SharedByUserID)
	}

	if err := store.RevokeMemeShare(meme.ID); err != nil {
		t.Fatal(err)
	}
	assertRedirectsToMeme("revoked", second.Slug)

	third, err := store.GetOrCreateMemeShare(meme.ID, "admin-1", now, now.Add(memeShareLifetime))
	if err != nil {
		t.Fatal(err)
	}
	if third.Slug == second.Slug || third.Slug == expired.Slug {
		t.Fatal("share after revoke reused an old link code")
	}
	assertRedirectsToMeme("revoked after re-share", second.Slug)
}

// Links issued before share links had codes carry a signed token. While valid
// they now serve the file directly, like the newer links.
func TestLegacySignedLinkServesFileUntilRevoked(t *testing.T) {
	server, store, meme := newShareTestServer(t)
	now := time.Now().UTC()
	share, err := store.GetOrCreateMemeShare(meme.ID, "admin-1", now, now.Add(30*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	token := url.QueryEscape(server.signMemeShare(share))
	get := func(suffix string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		server.handleMemeLink(recorder, httptest.NewRequest(http.MethodGet, "https://memes.example.com/m/"+meme.ID+suffix+"?share="+token, nil))
		return recorder
	}
	assertServesFile := func(label string) {
		t.Helper()
		for _, suffix := range []string{"", "/media/shared.png"} {
			recorder := get(suffix)
			if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Type") != "image/png" || recorder.Body.String() != "test image bytes" {
				t.Fatalf("%s %q: status = %d, content-type = %q", label, suffix, recorder.Code, recorder.Header().Get("Content-Type"))
			}
			if recorder.Header().Get("Accept-Ranges") != "bytes" {
				t.Fatalf("%s %q: no range support", label, suffix)
			}
		}
	}
	assertServesFile("active")

	// Renewing the share moves its expiry; the old token must survive that.
	if _, err := store.GetOrCreateMemeShare(meme.ID, "admin-1", now.Add(time.Minute), now.Add(memeShareLifetime)); err != nil {
		t.Fatal(err)
	}
	assertServesFile("renewed")

	if err := store.RevokeMemeShare(meme.ID); err != nil {
		t.Fatal(err)
	}
	if recorder := get(""); recorder.Code != http.StatusFound || recorder.Header().Get("Location") != "/m/"+meme.ID {
		t.Fatalf("revoked link status = %d, location = %q", recorder.Code, recorder.Header().Get("Location"))
	}
	if recorder := get("/media"); recorder.Code != http.StatusNotFound {
		t.Fatalf("revoked media status = %d", recorder.Code)
	}
}

func TestLegacyTokenIsNotHonouredPastItsOwnExpiry(t *testing.T) {
	server, store, meme := newShareTestServer(t)
	now := time.Now().UTC()
	share, err := store.GetOrCreateMemeShare(meme.ID, "admin-1", now.Add(-time.Hour), now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	token := server.signMemeShare(share)
	renewed, err := store.GetOrCreateMemeShare(meme.ID, "admin-1", now, now.Add(memeShareLifetime))
	if err != nil {
		t.Fatal(err)
	}
	if !server.validMemeShareToken(token, renewed, now) {
		t.Fatal("token rejected before its own expiry")
	}
	if server.validMemeShareToken(token, renewed, now.Add(2*time.Minute)) {
		t.Fatal("token outlived the expiry it was signed with")
	}
}

func TestRequestLogOmitsShareLinkCodes(t *testing.T) {
	if got := loggedRequestPath("/s/7c9e6679-7425-40de-944b-e07fc1f90ae7.png"); got != "/s/[share link]" {
		t.Fatalf("share link code reached the log: %q", got)
	}
	for _, path := range []string{"/", "/m/abc", "/static/app.js", "/share-target"} {
		if got := loggedRequestPath(path); got != path {
			t.Fatalf("loggedRequestPath(%q) = %q", path, got)
		}
	}
}
