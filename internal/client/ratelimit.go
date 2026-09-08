package client

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Per-identity throttles. "Identity" is the authenticated user when a request
// has already cleared auth, otherwise the client IP. Rates are sized so a
// human, a browser retrying, and the bundled bulk uploader all stay well under
// the ceiling while a runaway script or credential-stuffing loop is stopped.
const (
	// OAuth needs one or two requests to complete; more than a handful a minute
	// from one address is automated.
	authRatePerSecond = 0.5
	authRateBurst     = 10.0

	// Uploads arrive in batches, so allow a healthy burst but cap the sustained
	// rate so a loop cannot saturate disk and the tag-suggestion queue.
	writeRatePerSecond = 5.0
	writeRateBurst     = 40.0

	// Link imports spawn yt-dlp or remote fetches; keep these tight.
	linkImportRatePerSecond = 0.2
	linkImportRateBurst     = 6.0
)

const (
	rateLimiterIdleTTL       = 15 * time.Minute
	rateLimiterSweepInterval = 5 * time.Minute
)

// rateLimiter is a set of continuously-refilling token buckets keyed by an
// arbitrary identity string. It is safe for concurrent use.
type rateLimiter struct {
	ratePerSecond float64
	burst         float64
	now           func() time.Time // overridable in tests

	mu      sync.Mutex
	buckets map[string]*rateBucket
}

type rateBucket struct {
	tokens   float64
	lastSeen time.Time
}

func newRateLimiter(ratePerSecond, burst float64) *rateLimiter {
	if burst < 1 {
		burst = 1
	}
	return &rateLimiter{
		ratePerSecond: ratePerSecond,
		burst:         burst,
		now:           time.Now,
		buckets:       make(map[string]*rateBucket),
	}
}

// allow consumes one token for key and reports whether one was available. A nil
// limiter, or one configured with a non-positive rate, allows everything.
func (rl *rateLimiter) allow(key string) bool {
	if rl == nil || rl.ratePerSecond <= 0 {
		return true
	}

	now := rl.now()

	rl.mu.Lock()
	defer rl.mu.Unlock()

	bucket := rl.buckets[key]
	if bucket == nil {
		rl.buckets[key] = &rateBucket{tokens: rl.burst - 1, lastSeen: now}
		return true
	}

	if elapsed := now.Sub(bucket.lastSeen).Seconds(); elapsed > 0 {
		bucket.tokens += elapsed * rl.ratePerSecond
		if bucket.tokens > rl.burst {
			bucket.tokens = rl.burst
		}
		bucket.lastSeen = now
	}

	if bucket.tokens < 1 {
		return false
	}
	bucket.tokens -= 1
	return true
}

// sweep drops buckets that have not been touched within idleTTL so memory
// tracks active clients rather than every client ever seen.
func (rl *rateLimiter) sweep(idleTTL time.Duration) {
	if rl == nil {
		return
	}
	cutoff := rl.now().Add(-idleTTL)

	rl.mu.Lock()
	defer rl.mu.Unlock()
	for key, bucket := range rl.buckets {
		if bucket.lastSeen.Before(cutoff) {
			delete(rl.buckets, key)
		}
	}
}

func (rl *rateLimiter) size() int {
	if rl == nil {
		return 0
	}
	rl.mu.Lock()
	defer rl.mu.Unlock()
	return len(rl.buckets)
}

// clientIP returns the caller's address for abuse controls. Behind a reverse
// proxy every RemoteAddr is the proxy, so an operator terminating TLS upstream
// sets MEMEINDEX_CLIENT_IP_HEADER to the header their proxy populates (for
// example CF-Connecting-IP). A forwarding header is trusted only when it is
// explicitly named, so a spoofed X-Forwarded-For cannot mint unlimited buckets
// on a deployment that is reachable directly.
func (s *Server) clientIP(r *http.Request) string {
	if header := strings.TrimSpace(s.config.ClientIPHeader); header != "" {
		if raw := r.Header.Get(header); raw != "" {
			candidate := strings.TrimSpace(raw)
			if comma := strings.IndexByte(candidate, ','); comma >= 0 {
				candidate = strings.TrimSpace(candidate[:comma])
			}
			if candidate != "" {
				return candidate
			}
		}
	}
	if host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr)); err == nil {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}

// rateLimitIdentity keys a limiter by the authenticated user when the request
// has already passed auth (session in context), else by client IP. The
// prefixes keep the two namespaces from colliding.
func (s *Server) rateLimitIdentity(r *http.Request) string {
	if session, ok := sessionFromContext(r.Context()); ok {
		if id := strings.TrimSpace(session.UserID); id != "" {
			return "user:" + id
		}
	}
	return "ip:" + s.clientIP(r)
}

// enforceRateLimit consumes a token for scope+identity and, when the bucket is
// empty, writes a 429 JSON body and returns true so the caller returns at once.
func (s *Server) enforceRateLimit(w http.ResponseWriter, r *http.Request, limiter *rateLimiter, scope string) bool {
	if limiter == nil {
		return false
	}
	if limiter.allow(scope + "|" + s.rateLimitIdentity(r)) {
		return false
	}
	w.Header().Set("Retry-After", "60")
	writeJSON(w, http.StatusTooManyRequests, map[string]any{
		"error": "too many requests; slow down and try again shortly",
	})
	return true
}

// rateLimitByIP wraps a pre-auth handler (no session yet) and throttles it per
// client IP.
func (s *Server) rateLimitByIP(next http.Handler, limiter *rateLimiter, scope string) http.Handler {
	if limiter == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !limiter.allow(scope + "|ip:" + s.clientIP(r)) {
			w.Header().Set("Retry-After", "60")
			http.Error(w, "too many requests; slow down and try again shortly", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// startRateLimiterSweeper evicts idle buckets on an interval for the lifetime of
// the process.
func (s *Server) startRateLimiterSweeper() {
	limiters := []*rateLimiter{s.authLimiter, s.writeLimiter, s.linkImportLimiter}
	go func() {
		ticker := time.NewTicker(rateLimiterSweepInterval)
		defer ticker.Stop()
		for range ticker.C {
			for _, limiter := range limiters {
				limiter.sweep(rateLimiterIdleTTL)
			}
		}
	}()
}
