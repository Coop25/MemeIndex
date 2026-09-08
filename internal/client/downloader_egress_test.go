package client

import (
	"net/http"
	"net/http/httptest"
	"testing"
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
