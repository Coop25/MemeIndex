package client

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
)

// Log formats for MEMEINDEX_LOG_FORMAT. "text" keeps the plain log lines
// MemeIndex has always written, so existing log scraping keeps working;
// "json" emits one JSON object per line with structured fields (request id,
// status, duration, ...) for a log pipeline.
const (
	LogFormatText = "text"
	LogFormatJSON = "json"
)

// jsonLogging is set once at startup by ConfigureLogging.
var jsonLogging atomic.Bool

// ConfigureLogging installs the process-wide logger. In JSON mode every
// log.Printf in the codebase is routed through slog too, so all output
// shares one format. It returns the format in effect.
func ConfigureLogging(format string) string {
	if strings.EqualFold(strings.TrimSpace(format), LogFormatJSON) {
		slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
		jsonLogging.Store(true)
		return LogFormatJSON
	}
	jsonLogging.Store(false)
	return LogFormatText
}

type requestIDKey struct{}

// RequestIDHeader carries the request id back to the client, so a user's
// error report can be matched to the server's log lines.
const RequestIDHeader = "X-Request-ID"

// validRequestID accepts an inbound id from a proxy that already assigned
// one. Anything else is replaced, so a client cannot inject log content.
var validRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{8,64}$`)

func newRequestID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(b)
}

// withRequestID gives the request an id (an inbound X-Request-ID when it is
// well formed, else a fresh one), echoes it in the response, and stores it in
// the context for log lines written while handling the request.
func withRequestID(w http.ResponseWriter, r *http.Request) (*http.Request, string) {
	id := strings.TrimSpace(r.Header.Get(RequestIDHeader))
	if !validRequestID.MatchString(id) {
		id = newRequestID()
	}
	w.Header().Set(RequestIDHeader, id)
	return r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)), id
}

// RequestID returns the id LoggingMiddleware assigned to the request, or "".
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// requestLogger returns the default logger tagged with the request's id.
func requestLogger(r *http.Request) *slog.Logger {
	if id := RequestID(r.Context()); id != "" {
		return slog.Default().With("request_id", id)
	}
	return slog.Default()
}

// securityEvent records an authentication or authorization failure at WARN
// level with the client address and request id, so failed logins and
// permission probes are visible (and alertable) apart from ordinary request
// lines. Values that come from the request are passed as attributes, which
// both handlers escape.
func (s *Server) securityEvent(r *http.Request, event string, attrs ...any) {
	base := []any{
		"event", event,
		"client_ip", s.clientIP(r),
		"method", sanitizeLogField(r.Method),
		"path", sanitizeLogField(loggedRequestPath(r.URL.Path)),
	}
	if session, ok := sessionFromContext(r.Context()); ok && session.UserID != "" {
		base = append(base, "user_id", session.UserID)
	}
	requestLogger(r).Warn("security event", append(base, attrs...)...)
}
