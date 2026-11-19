package tagsuggest

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The server swaps http.DefaultTransport for a public-IP-only dialer to fence
// in the downloader. Ollama is on a private address, so the suggester must keep
// working whatever http.DefaultTransport has become (#68). This stands in a
// transport that refuses every dial; internal/client has the same check against
// the real pin.
func TestOllamaClientDoesNotUseDefaultTransport(t *testing.T) {
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":[{"name":"test-vision"}]}`))
	}))
	defer ollama.Close()

	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	http.DefaultTransport = &http.Transport{
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			return nil, errors.New("private and reserved network addresses are not allowed")
		},
	}

	service := New(Config{OllamaURL: ollama.URL, Model: "test-vision"})
	if err := service.Ready(context.Background()); err != nil {
		t.Fatalf("Ready went through http.DefaultTransport: %v", err)
	}
}

// HTTP(S)_PROXY in the container points at the downloader's allowlist proxy,
// which does not allow Ollama, so the Ollama client always goes direct.
func TestOllamaClientIgnoresProxyEnvironment(t *testing.T) {
	service := New(Config{OllamaURL: "http://ollama:11434", Model: "test-vision"})
	transport, ok := service.httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Ollama client transport = %T, want an explicit *http.Transport", service.httpClient.Transport)
	}
	if transport.Proxy != nil {
		t.Fatal("Ollama client honours the proxy environment; it must go direct")
	}
}
