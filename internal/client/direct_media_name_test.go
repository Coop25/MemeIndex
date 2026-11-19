package client

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"memeindex/internal/accessor"
)

// A direct-media import names the file after the final URL's decoded path, so
// a redirect to an escaped ?, # or space used to put those characters in the
// stored extension, leaving /uploads/<id><ext> a link that did not resolve.
func TestImportDirectMediaStoresURLSafeExtensionAfterRedirect(t *testing.T) {
	registerTestMIMETypes(t)
	s, _, _ := newShareTestServer(t)
	const source = "https://cdn.discordapp.com/attachments/123/456/clip.mp4"
	const body = "\x00\x00\x00\x18ftypmp42\x00\x00\x00\x00mp42isom redirected"
	client := &http.Client{Transport: mediaRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() == source {
			return &http.Response{StatusCode: http.StatusFound, Header: http.Header{
				"Location": {"https://media.example.com/files/clip.mp4%3Fsig=abc%23frag%20(1)"},
			}, Body: http.NoBody, Request: r}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/octet-stream"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}

	meme, err := s.importDirectMedia(context.Background(), client, accessor.AuditActor{}, source, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if meme.OriginalName != "clip.mp4?sig=abc#frag (1)" {
		t.Errorf("OriginalName = %q, want the decoded name kept for display", meme.OriginalName)
	}
	if meme.StoredName != meme.ID+".mp4" || meme.FilePath != "/uploads/"+meme.ID+".mp4" {
		t.Fatalf("stored as %q at %q, want %s.mp4", meme.StoredName, meme.FilePath, meme.ID)
	}

	uploads := s.withProtectedAssetAuth(http.StripPrefix("/uploads/", http.FileServer(http.Dir(s.managers.UploadDir()))))
	rec := httptest.NewRecorder()
	uploads.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, meme.FilePath, nil))
	if rec.Code != http.StatusOK || rec.Body.String() != body {
		t.Fatalf("GET %s: status %d, body %q", meme.FilePath, rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "video/mp4" {
		t.Errorf("GET %s: Content-Type = %q, want video/mp4", meme.FilePath, got)
	}

	// The share path serves the same stored file.
	rec = httptest.NewRecorder()
	s.serveSharedMemeAsset(rec, httptest.NewRequest(http.MethodGet, "/s/x", nil), meme, false, 0)
	if rec.Code != http.StatusOK || rec.Body.String() != body {
		t.Fatalf("shared asset: status %d, body %q", rec.Code, rec.Body.String())
	}
}
