package client

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Share link lines say who fetched the file and through which Cloudflare data
// center, so it shows whether Discord's request reached this server at all.
func TestShareLinkLogLineNamesRequesterAndDataCenter(t *testing.T) {
	var buf bytes.Buffer
	previous := log.Writer()
	flags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(previous)
		log.SetFlags(flags)
	})
	handler := LoggingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	serve := func(path, rayID, userAgent string) string {
		buf.Reset()
		request := httptest.NewRequest(http.MethodGet, "https://memes.example.com"+path, nil)
		request.Header.Set("Cf-Ray", rayID)
		request.Header.Set("User-Agent", userAgent)
		handler.ServeHTTP(httptest.NewRecorder(), request)
		return strings.TrimSuffix(buf.String(), "\n")
	}

	discord := "Mozilla/5.0 (compatible; Discordbot/2.0; +https://discordapp.com)"
	line := serve("/s/7c9e6679-7425-40de-944b-e07fc1f90ae7.mp4", "8f1c2a3b4c5d6e7f-IAD", discord)
	if !strings.HasPrefix(line, "GET /s/[share link] 200 (") || !strings.HasSuffix(line, ` colo=IAD ua="`+discord+`"`) {
		t.Fatalf("share link line = %q", line)
	}
	// A missing or malformed ray ID and an oversized, hostile User-Agent.
	line = serve("/s/7c9e6679-7425-40de-944b-e07fc1f90ae7.mp4", "not-a-ray\r\nGET /forged", strings.Repeat("x", 300)+"\"\r\nGET /forged")
	if !strings.Contains(line, " colo=- ") || strings.ContainsAny(line, "\r\n") || strings.Contains(line, "/forged") || len(line) > 260 {
		t.Fatalf("hostile headers reached the log: %q", line)
	}
	// Other paths keep their short line.
	if line := serve("/static/app.js", "8f1c2a3b4c5d6e7f-IAD", discord); strings.Contains(line, "colo=") || strings.Contains(line, "ua=") {
		t.Fatalf("non-share line = %q", line)
	}
}

func TestLoggingMiddlewareStripsRequestLineControlCharacters(t *testing.T) {
	var buf bytes.Buffer
	previous := log.Writer()
	flags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(previous)
		log.SetFlags(flags)
	})

	handler := LoggingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	request := httptest.NewRequest(http.MethodGet, "https://example.com/", nil)
	// A path with an embedded CRLF and a forged log line.
	request.URL.Path = "/legit\r\n2026/01/01 00:00:00 GET /admin/forged (0s)"
	handler.ServeHTTP(httptest.NewRecorder(), request)

	logged := buf.String()
	if strings.Contains(logged, "\n2026/01/01 00:00:00 GET /admin/forged") {
		t.Fatalf("log line was not sanitized: %q", logged)
	}
	if strings.ContainsAny(strings.TrimSuffix(logged, "\n"), "\r\n") {
		t.Fatalf("sanitized log line still contains newlines: %q", logged)
	}
}
