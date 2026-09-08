package client

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newBody(size int) []byte {
	b := make([]byte, size)
	for i := range b {
		b[i] = byte('a' + i%26)
	}
	return b
}

func serveThrough(t *testing.T, handler http.Handler, req *http.Request) *http.Response {
	t.Helper()
	rec := httptest.NewRecorder()
	AssetCompression(handler).ServeHTTP(rec, req)
	return rec.Result()
}

func TestAssetCompressionGzipsTextAssets(t *testing.T) {
	body := newBody(4096)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		_, _ = w.Write(body)
	})

	req := httptest.NewRequest(http.MethodGet, "/static/app.js?h=abc", nil)
	req.Header.Set("Accept-Encoding", "gzip, deflate, br")
	resp := serveThrough(t, handler, req)

	if got := resp.Header.Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	if resp.Header.Get("Content-Length") != "" {
		t.Fatalf("Content-Length must be dropped when the body is re-encoded")
	}
	if vary := resp.Header.Get("Vary"); !strings.Contains(strings.ToLower(vary), "accept-encoding") {
		t.Fatalf("Vary = %q, want it to include Accept-Encoding", vary)
	}

	gzr, err := gzip.NewReader(resp.Body)
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	got, err := io.ReadAll(gzr)
	if err != nil {
		t.Fatalf("read gzip body: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("decompressed body does not match original")
	}
}

func TestAssetCompressionNeverTouchesProtectedMedia(t *testing.T) {
	// A hostile handler that mislabels an upload as text must still not be
	// compressed: the /uploads/ and /thumbnails/ trees bypass the compressor
	// wholesale so private media can never be buffered or re-encoded here.
	for _, path := range []string{"/uploads/secret.png", "/thumbnails/secret.jpg"} {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write(newBody(8192))
		})
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Accept-Encoding", "gzip")
		resp := serveThrough(t, handler, req)
		if resp.Header.Get("Content-Encoding") != "" {
			t.Fatalf("%s: response was compressed; protected media must bypass the compressor", path)
		}
	}
}

func TestAssetCompressionSkipsNonCompressibleAndOptOut(t *testing.T) {
	cases := []struct {
		name        string
		contentType string
		accept      string
		method      string
		rangeHeader string
		status      int
	}{
		{name: "json api body", contentType: "application/json", accept: "gzip", method: http.MethodGet, status: 200},
		{name: "png", contentType: "image/png", accept: "gzip", method: http.MethodGet, status: 200},
		{name: "no gzip offered", contentType: "text/css", accept: "identity", method: http.MethodGet, status: 200},
		{name: "gzip disabled by q=0", contentType: "text/css", accept: "gzip;q=0", method: http.MethodGet, status: 200},
		{name: "range request", contentType: "text/css", accept: "gzip", method: http.MethodGet, rangeHeader: "bytes=0-10", status: 200},
		{name: "not modified", contentType: "text/css", accept: "gzip", method: http.MethodGet, status: 304},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				w.WriteHeader(tc.status)
				if tc.status == http.StatusOK {
					_, _ = w.Write(newBody(4096))
				}
			})
			req := httptest.NewRequest(tc.method, "/static/thing?h=1", nil)
			req.Header.Set("Accept-Encoding", tc.accept)
			if tc.rangeHeader != "" {
				req.Header.Set("Range", tc.rangeHeader)
			}
			resp := serveThrough(t, handler, req)
			if resp.Header.Get("Content-Encoding") == "gzip" {
				t.Fatalf("%s: response was gzipped but should not have been", tc.name)
			}
		})
	}
}

func TestStaticAssetHandlerMarksFingerprintedURLsImmutable(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app.js"), []byte("console.log(1)"), 0o644); err != nil {
		t.Fatal(err)
	}
	handler := staticAssetHandler(dir)

	withHash := httptest.NewRecorder()
	handler.ServeHTTP(withHash, httptest.NewRequest(http.MethodGet, "/static/app.js?v=1&h=deadbeef", nil))
	if withHash.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", withHash.Code)
	}
	if cc := withHash.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") || !strings.Contains(cc, "private") {
		t.Fatalf("fingerprinted asset Cache-Control = %q, want private + immutable", cc)
	}

	plain := httptest.NewRecorder()
	handler.ServeHTTP(plain, httptest.NewRequest(http.MethodGet, "/static/app.js", nil))
	if cc := plain.Header().Get("Cache-Control"); cc != "" {
		t.Fatalf("unfingerprinted asset Cache-Control = %q, want none set here", cc)
	}
}
