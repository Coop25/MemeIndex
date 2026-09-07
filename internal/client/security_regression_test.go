package client

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOAuthStateRequiresInitiatingBrowserAndCannotBeReplayed(t *testing.T) {
	a := newAuthService(DiscordAuthConfig{ClientID: "client", ClientSecret: "secret", RedirectURL: "https://example.com/auth/callback", SessionSecret: "test-secret"}, nil)
	a.rememberState("browser-a")
	r := httptest.NewRequest(http.MethodGet, "/auth/callback?state=browser-a", nil)
	if a.consumeValidState(r) {
		t.Fatal("accepted callback without browser cookie")
	}
	w := httptest.NewRecorder()
	a.setStateCookie(w, r, "browser-b")
	r.AddCookie(w.Result().Cookies()[0])
	if a.consumeValidState(r) {
		t.Fatal("accepted another browser's state")
	}
	r.Header.Del("Cookie")
	w = httptest.NewRecorder()
	a.setStateCookie(w, r, "browser-a")
	r.AddCookie(w.Result().Cookies()[0])
	if !a.consumeValidState(r) {
		t.Fatal("rejected initiating browser after unrelated invalid callbacks")
	}
	if a.consumeValidState(r) {
		t.Fatal("accepted replay with signed cookie")
	}
	a.pendingStates["browser-a"] = time.Now().Add(-time.Minute)
	if a.consumeValidState(r) {
		t.Fatal("accepted expired state with signed cookie")
	}
}

func TestSharedResponsesRejectForwardedHostAndDisableCaching(t *testing.T) {
	s, store, meme := newShareTestServer(t)
	now := time.Now().UTC()
	share, err := store.GetOrCreateMemeShare(meme.ID, "viewer", now, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", "/media/shared.png", "/preview/preview.png"} {
		r := httptest.NewRequest(http.MethodGet, "https://memes.example.com/m/"+meme.ID+suffix+"?share="+s.signMemeShare(share), nil)
		r.Header.Set("X-Forwarded-Host", "attacker.invalid")
		w := httptest.NewRecorder()
		s.handleMemeLink(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: status %d", suffix, w.Code)
		}
		if strings.Contains(w.Body.String(), "attacker.invalid") {
			t.Fatal("untrusted forwarded host reflected into shared page")
		}
		for _, header := range []string{"Cache-Control", "CDN-Cache-Control", "Cloudflare-CDN-Cache-Control", "Surrogate-Control"} {
			if !strings.Contains(w.Header().Get(header), "no-store") {
				t.Fatalf("%s: %s permits caching", suffix, header)
			}
		}
	}
}
