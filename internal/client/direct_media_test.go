package client

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"memeindex/internal/accessor"
)

type mediaRoundTripper func(*http.Request) (*http.Response, error)

func (f mediaRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDirectMediaURL(t *testing.T) {
	for _, raw := range []string{
		"https://cdn.discordapp.com/attachments/123/456/file.mp4?ex=abc&is=def&hm=123&",
		"https://media.discordapp.net/attachments/123/456/file.PNG?width=100",
		"https://example.com/picture.webp", "https://example.com/animation.gif",
	} {
		if !isDirectMediaURL(raw) {
			t.Errorf("rejected %s", raw)
		}
	}
	for _, raw := range []string{"file:///image.png", "https://user:pass@example.com/a.mp4", "https://example.com/post", "https://example.com/a.html?name=a.png"} {
		if isDirectMediaURL(raw) {
			t.Errorf("accepted %s", raw)
		}
	}
}

func TestImportDirectMediaStoresFileAndSignedSource(t *testing.T) {
	for _, tc := range []struct{ ext, body, mime string }{
		{"png", "\x89PNG\r\n\x1a\nimage data", "image/png"},
		{"gif", "GIF89aimage data", "image/gif"},
		{"mp4", "\x00\x00\x00\x18ftypmp42\x00\x00\x00\x00mp42isom", "video/mp4"},
	} {
		t.Run(tc.ext, func(t *testing.T) {
			s, _, _ := newShareTestServer(t)
			source := "https://cdn.discordapp.com/attachments/123/456/file." + tc.ext + "?ex=abc&is=def&hm=signature&"
			client := &http.Client{Transport: mediaRoundTripper(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() != source {
					t.Fatalf("signed URL changed: %s", r.URL)
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/octet-stream"}}, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})}
			meme, err := s.importDirectMedia(context.Background(), client, accessor.AuditActor{}, source, []string{"discord"}, "notes")
			if err != nil {
				t.Fatal(err)
			}
			if meme.SourceURL != source || meme.ContentType != tc.mime || meme.OriginalName != "file."+tc.ext || meme.Notes != "notes" {
				t.Fatalf("unexpected meme: %+v", meme)
			}
			_, err = s.importDirectMedia(context.Background(), client, accessor.AuditActor{}, source, nil, "")
			var duplicate *accessor.DuplicateMemeError
			if !errors.As(err, &duplicate) {
				t.Fatalf("expected duplicate, got %v", err)
			}
		})
	}
}

func TestImportDirectMediaRejectsInvalidResponses(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		size   int64
	}{
		{"expired", 403, "denied", 6},
		{"missing", 404, "missing", 7},
		{"html", 200, "<!doctype html><html>error</html>", -1},
		{"empty", 200, "", 0},
		{"oversized", 200, "", maxDirectMediaBytes + 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{}
			client := &http.Client{Transport: mediaRoundTripper(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Content-Type": {"image/png"}}, Body: io.NopCloser(strings.NewReader(tc.body)), ContentLength: tc.size}, nil
			})}
			if _, err := s.importDirectMedia(context.Background(), client, accessor.AuditActor{}, "https://example.com/image.png", nil, ""); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
}

func TestDirectMediaBlocksPrivateNetwork(t *testing.T) {
	client := directMediaHTTPClient()
	defer client.CloseIdleConnections()
	if _, err := client.Get("http://127.0.0.1/image.png"); err == nil {
		t.Fatal("private network request succeeded")
	}
}
