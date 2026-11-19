package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"memeindex/internal/tagsuggest"
)

// With no forward proxy configured the downloader transport is pinned; when a
// proxy is configured the proxy is the egress boundary and the transport is left
// alone.
func TestShouldPinDownloaderTransport(t *testing.T) {
	if !shouldPinDownloaderTransport("") {
		t.Fatal("expected pinning when no proxy is configured")
	}
	if !shouldPinDownloaderTransport("   ") {
		t.Fatal("expected pinning when proxy is blank")
	}
	if shouldPinDownloaderTransport("http://egress-proxy:8888") {
		t.Fatal("expected no pinning when a proxy is configured")
	}
}

// The pinned transport that mediafetch's un-injectable http.DefaultClient calls
// inherit refuses to connect to a private/loopback host, so a downloader
// redirect or media URL cannot be steered at an internal service.
func TestPublicRemoteTransportRefusesLoopbackHost(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	transport := newPublicRemoteTransport()
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}

	resp, err := client.Get(backend.URL) // 127.0.0.1
	if err == nil {
		resp.Body.Close()
		t.Fatalf("loopback request unexpectedly succeeded via pinned transport")
	}
}

// pinDownloaderTransportForTest runs the real startup pin and undoes it when
// the test ends. pinDownloaderTransportOnce is a sync.Once, so it is reset on
// both sides: before, so the pin actually applies in this test, and after, so
// the next caller starts from the restored http.DefaultTransport. Tests using
// it must not run in parallel, since http.DefaultTransport is process-wide.
func pinDownloaderTransportForTest(t *testing.T) {
	t.Helper()
	original := http.DefaultTransport
	pinDownloaderTransportOnce = sync.Once{}
	t.Cleanup(func() {
		http.DefaultTransport = original
		pinDownloaderTransportOnce = sync.Once{}
	})
	pinDownloaderDefaultTransport("")
	if http.DefaultTransport == original {
		t.Fatal("pinDownloaderDefaultTransport did not replace http.DefaultTransport")
	}
}

// Ollama normally lives on a private Docker or LAN address. The downloader pin
// must not reach the tag suggester, or the worker sits in waiting_for_ollama
// forever (#68).
func TestTagSuggesterReachesPrivateOllamaAfterDownloaderPin(t *testing.T) {
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":[{"name":"test-vision","model":"test-vision"}]}`))
	}))
	defer ollama.Close()

	pinDownloaderTransportForTest(t)

	// Prove the pin is live: a client that leaves Transport nil is refused.
	if resp, err := (&http.Client{}).Get(ollama.URL + "/api/tags"); err == nil {
		resp.Body.Close()
		t.Fatal("nil-Transport client reached loopback after pinning; the test is not exercising the pin")
	}

	service := tagsuggest.New(tagsuggest.Config{OllamaURL: ollama.URL, Model: "test-vision"})
	if err := service.Ready(context.Background()); err != nil {
		t.Fatalf("tag suggester could not reach loopback Ollama after pinning: %v", err)
	}
}

// Discord login must not ride the container's HTTP(S)_PROXY: that is the
// downloader's media-host allowlist proxy and does not allow discord.com (#68).
// net/http reads the proxy environment only once per process, so this checks
// the transport's Proxy func rather than setting HTTPS_PROXY mid-run. The
// webhook client has the same check in share_warm_test.go.
func TestDiscordAuthClientIgnoresProxyEnvironment(t *testing.T) {
	auth := newAuthService(DiscordAuthConfig{ClientID: "c", ClientSecret: "s", RedirectURL: "https://example.com/auth/callback", SessionSecret: "test"}, nil)
	transport, ok := auth.client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Discord auth client transport = %T, want an explicit *http.Transport", auth.client.Transport)
	}
	if transport == http.DefaultTransport {
		t.Fatal("Discord auth client shares http.DefaultTransport")
	}
	if transport.Proxy != nil {
		t.Fatal("Discord auth client honours the proxy environment; it must go direct")
	}
}
