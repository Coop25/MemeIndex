package client

import (
	"bytes"
	"encoding/json"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// captureJSONLogs switches the process logger to JSON into a buffer for the
// rest of the test.
func captureJSONLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	previous := slog.Default()
	// slog.SetDefault also redirects the log package, and restoring the
	// default slog handler does not undo that, so put it back by hand.
	logWriter, logFlags := log.Writer(), log.Flags()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	jsonLogging.Store(true)
	t.Cleanup(func() {
		slog.SetDefault(previous)
		log.SetOutput(logWriter)
		log.SetFlags(logFlags)
		jsonLogging.Store(false)
	})
	return &buf
}

func decodeLogLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("log line is not JSON: %q (%v)", line, err)
		}
		out = append(out, entry)
	}
	return out
}

func TestConfigureLoggingDefaultsToText(t *testing.T) {
	t.Cleanup(func() { jsonLogging.Store(false) })
	for _, format := range []string{"", "text", "TEXT"} {
		if got := ConfigureLogging(format); got != LogFormatText || jsonLogging.Load() {
			t.Fatalf("ConfigureLogging(%q) = %q json=%v, want text", format, got, jsonLogging.Load())
		}
	}
}

func TestValidateSecurityRejectsUnknownLogFormat(t *testing.T) {
	c := Config{AllowAnonymous: true, MaxUploadBytes: 1, LogFormat: "xml"}
	if err := c.validateSecurity(); err == nil || !strings.Contains(err.Error(), "MEMEINDEX_LOG_FORMAT") {
		t.Fatalf("err = %v, want a MEMEINDEX_LOG_FORMAT error", err)
	}
}

// In JSON mode a request is one structured record carrying the same request
// id the client receives, and a log.Printf from a handler shares the format.
func TestJSONRequestLogCarriesRequestID(t *testing.T) {
	buf := captureJSONLogs(t)
	handler := LoggingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("inside handler")
		requestLogger(r).Info("handler detail")
		w.WriteHeader(http.StatusTeapot)
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/memes?q=secret", nil))

	id := rec.Header().Get(RequestIDHeader)
	if !validRequestID.MatchString(id) {
		t.Fatalf("response request id = %q", id)
	}
	entries := decodeLogLines(t, buf)
	if len(entries) != 3 {
		t.Fatalf("got %d log records, want 3: %s", len(entries), buf.String())
	}
	if entries[0]["msg"] != "inside handler" {
		t.Errorf("log.Printf record = %v, want it routed through slog", entries[0])
	}
	if entries[1]["request_id"] != id {
		t.Errorf("handler record request_id = %v, want %q", entries[1]["request_id"], id)
	}
	req := entries[2]
	if req["msg"] != "request" || req["request_id"] != id || req["path"] != "/api/memes" || req["status"] != float64(http.StatusTeapot) {
		t.Errorf("request record = %v", req)
	}
	if strings.Contains(buf.String(), "secret") {
		t.Errorf("query string reached the log: %s", buf.String())
	}
}

// A well-formed id from an upstream proxy is kept; anything else is replaced
// so a client cannot write into the log through it.
func TestRequestIDAcceptsOnlyWellFormedInboundIDs(t *testing.T) {
	handler := LoggingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	serve := func(inbound string) string {
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.Header.Set(RequestIDHeader, inbound)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Header().Get(RequestIDHeader)
	}
	if got := serve("proxy-assigned-1234"); got != "proxy-assigned-1234" {
		t.Errorf("well-formed id = %q, want it kept", got)
	}
	for _, hostile := range []string{"short", "has space in it", "line\r\nbreak-forged", strings.Repeat("a", 65)} {
		if got := serve(hostile); got == hostile || !validRequestID.MatchString(got) {
			t.Errorf("inbound %q -> %q, want a fresh id", hostile, got)
		}
	}
}

// The plain text request line is unchanged from before structured logging.
func TestTextRequestLineIsUnchanged(t *testing.T) {
	var buf bytes.Buffer
	previous, flags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() { log.SetOutput(previous); log.SetFlags(flags) })

	handler := LoggingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/memes", nil))
	line := strings.TrimSpace(buf.String())
	if !strings.HasPrefix(line, "GET /api/memes 200 (") || strings.Contains(line, "request_id") {
		t.Fatalf("text line = %q", line)
	}
}

func TestSecurityEventRecordsClientAndReason(t *testing.T) {
	buf := captureJSONLogs(t)
	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/auth/callback?state=x", nil)
	req.RemoteAddr = "203.0.113.9:4444"
	s.securityEvent(req, "login_invalid_state")

	entries := decodeLogLines(t, buf)
	if len(entries) != 1 {
		t.Fatalf("got %d records: %s", len(entries), buf.String())
	}
	e := entries[0]
	if e["level"] != "WARN" || e["event"] != "login_invalid_state" || e["client_ip"] != "203.0.113.9" || e["path"] != "/auth/callback" {
		t.Fatalf("security record = %v", e)
	}
}
