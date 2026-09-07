package client

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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
