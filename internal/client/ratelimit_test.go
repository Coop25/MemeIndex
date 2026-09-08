package client

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"memeindex/internal/accessor"
	"memeindex/internal/manager"
)

func TestRateLimiterAllowsBurstThenBlocks(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	rl := newRateLimiter(1, 3)
	rl.now = func() time.Time { return base }

	for i := 0; i < 3; i++ {
		if !rl.allow("k") {
			t.Fatalf("request %d within burst was blocked", i+1)
		}
	}
	if rl.allow("k") {
		t.Fatal("request past burst was allowed")
	}
}

func TestRateLimiterRefillsOverTime(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	rl := newRateLimiter(2, 2) // 2 tokens/sec
	rl.now = func() time.Time { return now }

	if !rl.allow("k") || !rl.allow("k") {
		t.Fatal("burst tokens should be available")
	}
	if rl.allow("k") {
		t.Fatal("bucket should be empty")
	}

	now = now.Add(500 * time.Millisecond) // one token refilled
	if !rl.allow("k") {
		t.Fatal("token should have refilled after 500ms")
	}
	if rl.allow("k") {
		t.Fatal("only one token should have refilled")
	}
}

func TestRateLimiterKeysAreIndependent(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	rl := newRateLimiter(1, 1)
	rl.now = func() time.Time { return base }

	if !rl.allow("a") {
		t.Fatal("first key should be allowed")
	}
	if !rl.allow("b") {
		t.Fatal("second key must not share the first key's bucket")
	}
	if rl.allow("a") {
		t.Fatal("first key bucket should now be empty")
	}
}

func TestRateLimiterNilAndZeroRateAllowEverything(t *testing.T) {
	var nilLimiter *rateLimiter
	for i := 0; i < 100; i++ {
		if !nilLimiter.allow("k") {
			t.Fatal("nil limiter must allow all")
		}
	}

	off := newRateLimiter(0, 5)
	for i := 0; i < 100; i++ {
		if !off.allow("k") {
			t.Fatal("non-positive-rate limiter must allow all")
		}
	}
}

func TestRateLimiterSweepEvictsIdleBuckets(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	rl := newRateLimiter(1, 1)
	rl.now = func() time.Time { return now }

	rl.allow("stale")
	now = now.Add(30 * time.Minute)
	rl.allow("fresh")

	rl.sweep(15 * time.Minute)

	if got := rl.size(); got != 1 {
		t.Fatalf("bucket count after sweep = %d, want 1", got)
	}
	if !rl.allow("stale") {
		t.Fatal("evicted key should start with a full bucket")
	}
}

func TestClientIPUsesConfiguredHeaderThenRemoteAddr(t *testing.T) {
	withHeader := &Server{config: Config{ClientIPHeader: "CF-Connecting-IP"}}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:5555"
	r.Header.Set("CF-Connecting-IP", "203.0.113.7")
	if got := withHeader.clientIP(r); got != "203.0.113.7" {
		t.Fatalf("clientIP with header = %q, want 203.0.113.7", got)
	}

	r.Header.Set("CF-Connecting-IP", "198.51.100.4, 203.0.113.7")
	if got := withHeader.clientIP(r); got != "198.51.100.4" {
		t.Fatalf("clientIP with list header = %q, want 198.51.100.4", got)
	}

	noHeader := &Server{config: Config{}}
	r2 := httptest.NewRequest(http.MethodGet, "/", nil)
	r2.RemoteAddr = "192.0.2.9:41000"
	r2.Header.Set("X-Forwarded-For", "1.2.3.4") // untrusted, must be ignored
	if got := noHeader.clientIP(r2); got != "192.0.2.9" {
		t.Fatalf("clientIP without configured header = %q, want 192.0.2.9", got)
	}
}

func TestEnforceRateLimitWrites429AndPrefersUserIdentity(t *testing.T) {
	s := &Server{writeLimiter: newRateLimiter(0.0001, 1)}

	makeReq := func(userID, remote string) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/api/memes", nil)
		r.RemoteAddr = remote
		if userID != "" {
			r = r.WithContext(contextWithSession(r.Context(), authSession{UserID: userID}))
		}
		return r
	}

	rec := httptest.NewRecorder()
	if s.enforceRateLimit(rec, makeReq("alice", "10.0.0.1:1"), s.writeLimiter, "upload") {
		t.Fatal("first request for alice should pass")
	}

	rec = httptest.NewRecorder()
	if !s.enforceRateLimit(rec, makeReq("alice", "10.0.0.2:1"), s.writeLimiter, "upload") {
		t.Fatal("second request for alice should be limited regardless of changing IP")
	}
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("429 response must carry Retry-After")
	}

	// A different user is unaffected by alice's exhausted bucket.
	rec = httptest.NewRecorder()
	if s.enforceRateLimit(rec, makeReq("bob", "10.0.0.1:1"), s.writeLimiter, "upload") {
		t.Fatal("bob should not share alice's bucket")
	}
}

func TestEnforceRateLimitNilLimiterIsNoop(t *testing.T) {
	s := &Server{}
	rec := httptest.NewRecorder()
	for i := 0; i < 50; i++ {
		if s.enforceRateLimit(rec, httptest.NewRequest(http.MethodPost, "/api/memes", nil), s.writeLimiter, "upload") {
			t.Fatal("nil limiter must never limit")
		}
	}
}

func TestRateLimitByIPMiddlewareBlocksAfterBurst(t *testing.T) {
	limiter := newRateLimiter(0.0001, 2)
	s := &Server{authLimiter: limiter}
	var served int
	handler := s.rateLimitByIP(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { served++ }), limiter, "auth")

	call := func() int {
		r := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
		r.RemoteAddr = "203.0.113.10:2222"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)
		return rec.Code
	}

	if code := call(); code != http.StatusOK {
		t.Fatalf("call 1 code = %d", code)
	}
	if code := call(); code != http.StatusOK {
		t.Fatalf("call 2 code = %d", code)
	}
	if code := call(); code != http.StatusTooManyRequests {
		t.Fatalf("call 3 code = %d, want 429", code)
	}
	if served != 2 {
		t.Fatalf("wrapped handler served %d times, want 2", served)
	}
}

func TestRoutesThrottleRepeatedLogins(t *testing.T) {
	store, err := accessor.NewMemeStore(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	s := &Server{
		managers: manager.NewMemeManager(store),
		auth: newAuthService(DiscordAuthConfig{
			ClientID:      "id",
			ClientSecret:  "secret",
			RedirectURL:   "https://example.com/auth/callback",
			SessionSecret: "0123456789abcdef0123456789abcdef",
		}, nil),
		shareSecret: []byte("0123456789abcdef0123456789abcdef"),
		authLimiter: newRateLimiter(0.0001, 3),
	}
	handler := s.Routes()

	var lastCode int
	for i := 0; i < 6; i++ {
		r := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
		r.RemoteAddr = "198.51.100.23:9000"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)
		lastCode = rec.Code
	}
	if lastCode != http.StatusTooManyRequests {
		t.Fatalf("repeated logins ended with %d, want 429", lastCode)
	}
}
