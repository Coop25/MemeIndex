package client

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// newCSPNonce returns a fresh base64 nonce for a per-response
// script-src 'nonce-...' directive. It falls back to an empty string only if the
// system CSPRNG fails, in which case the caller's inline script is blocked
// (fail closed).
func newCSPNonce() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(buf)
}

// loggedRequestPath is the request path as it should appear in the log. The
// code in a /s/ share link is all it takes to fetch the shared file, so it is
// kept out of the log the same way the older links' ?share= token always was
// (query strings are never logged).
func loggedRequestPath(path string) string {
	if strings.HasPrefix(path, "/s/") {
		return "/s/[share link]"
	}
	return path
}

func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Health probes are polled every few seconds by orchestration; logging
		// them would drown out real request traffic.
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		// Sanitize request-derived fields so a client cannot inject newlines and
		// forge additional log lines.
		line := fmt.Sprintf("%s %s %d (%s)", sanitizeLogField(r.Method), sanitizeLogField(loggedRequestPath(r.URL.Path)), recorder.status, time.Since(start).Round(time.Millisecond))
		if strings.HasPrefix(r.URL.Path, "/s/") {
			// Say who fetched a shared file and through which Cloudflare data
			// center, so a Discord fetch that missed the cache is plain to see
			// (cache hits never reach this server). Quoting escapes anything a
			// client could use to forge a log line.
			line += " colo=" + cloudflareColo(r.Header.Get("Cf-Ray")) + " ua=" + strconv.Quote(truncateForLog(r.Header.Get("User-Agent"), 120))
		}
		log.Print(line)
	})
}

// cloudflareColo is the data center code at the end of a Cloudflare ray ID
// ("8f1c2a3b4c5d6e7f-ORD"), or "-" when there is none. With Tiered Cache on,
// it can name the upper-tier data center rather than the visitor's.
func cloudflareColo(rayID string) string {
	colo := rayID[strings.LastIndex(rayID, "-")+1:]
	if len(colo) != 3 {
		return "-"
	}
	for _, r := range colo {
		if r < 'A' || r > 'Z' {
			return "-"
		}
	}
	return colo
}

func truncateForLog(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return strings.ToValidUTF8(value[:limit], "") + "…"
}

// sanitizeLogField strips control characters (including CR and LF) from a value
// before it is written to a log line.
func sanitizeLogField(value string) string {
	if !strings.ContainsFunc(value, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return value
	}
	var b strings.Builder
	b.Grow(len(value))
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			b.WriteByte(' ')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// appContentSecurityPolicy is the default-deny CSP applied to first-party HTML,
// scripts, and other same-origin responses. The SPA loads no inline scripts, so
// script execution is restricted to same-origin files. Inline styles remain
// allowed because the frontend sets style attributes/custom properties directly.
const appContentSecurityPolicy = "default-src 'self'; " +
	"script-src 'self'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' blob: data: https://cdn.discordapp.com; " +
	"media-src 'self' blob:; " +
	"font-src 'self'; " +
	"connect-src 'self'; " +
	"worker-src 'self'; " +
	"manifest-src 'self'; " +
	"frame-ancestors 'none'; " +
	"base-uri 'none'; " +
	"form-action 'self'; " +
	"object-src 'none'"

// apiContentSecurityPolicy locks down JSON/redirect responses that never need to
// load or embed any resource.
const apiContentSecurityPolicy = "default-src 'none'; frame-ancestors 'none'; base-uri 'none'"

func contentSecurityPolicyForPath(path string) string {
	if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/auth/") {
		return apiContentSecurityPolicy
	}
	return appContentSecurityPolicy
}

// User-specific API data and login redirects must not survive access revocation.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		// Baseline CSP for every response. Handlers that serve untrusted uploads,
		// the public share page, or the access-denied page override this with a
		// stricter or nonce-based policy of their own.
		w.Header().Set("Content-Security-Policy", contentSecurityPolicyForPath(r.URL.Path))
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/auth/") {
			setProtectedAssetHeaders(w)
		}
		if strings.HasPrefix(r.URL.Path, "/api/") && r.Body != nil &&
			!(r.URL.Path == "/api/memes" && r.Method == http.MethodPost) &&
			r.URL.Path != "/api/admin/backup/import" &&
			(r.URL.Path != "/api/admin/backup/import/parts" || r.Method != http.MethodPut) &&
			(!strings.HasPrefix(r.URL.Path, "/api/audiobooks/uploads/") || r.Method != http.MethodPatch) {
			r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		}
		next.ServeHTTP(w, r)
	})
}
