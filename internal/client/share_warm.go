package client

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"memeindex/internal/accessor"
)

// Right after a share is created, the server fetches the new link once
// through its own public origin, so Cloudflare already holds the file when a
// chat app asks for it. Cloudflare caches per data center, so this only helps
// a chat app whose fetch reaches the same data center as this server's
// request, or the same upper tier when Tiered Cache is on.

const (
	// shareWarmTimeout bounds one warm-up, including reading the file.
	shareWarmTimeout = 10 * time.Minute
	// shareWarmMaxConcurrent caps warm-ups in flight, so a burst of shares
	// does not saturate the server's upload.
	shareWarmMaxConcurrent = 2
	// cloudflareMaxCacheableBytes is the largest file Cloudflare caches on the
	// Free, Pro and Business plans; warming anything bigger would upload it
	// for nothing.
	cloudflareMaxCacheableBytes = 512 << 20
)

type shareCacheWarmer struct {
	// client sends the warm-up requests; nil means newShareWarmClient.
	client *http.Client

	mu       sync.Mutex
	inFlight map[string]struct{}
}

func newShareWarmClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// The container's HTTP(S)_PROXY sends traffic through the downloader's
	// egress allowlist, which does not include this site.
	transport.Proxy = nil
	return &http.Client{
		Transport: transport,
		Timeout:   shareWarmTimeout,
		// A redirect means the link no longer serves its file; following it
		// would only fetch the sign-in page.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func (w *shareCacheWarmer) httpClient() *http.Client {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.client == nil {
		w.client = newShareWarmClient()
	}
	return w.client
}

// begin claims link for a warm-up. It reports false when that link is already
// being warmed or too many warm-ups are running.
func (w *shareCacheWarmer) begin(link string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, running := w.inFlight[link]; running || len(w.inFlight) >= shareWarmMaxConcurrent {
		return false
	}
	if w.inFlight == nil {
		w.inFlight = map[string]struct{}{}
	}
	w.inFlight[link] = struct{}{}
	return true
}

func (w *shareCacheWarmer) end(link string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.inFlight, link)
}

// warmShareLink fetches a share link once in the background so Cloudflare
// caches it. It only fetches links on the configured public origin: without
// one, links are built from the request's own Host header, and the server
// must never be made to fetch whatever host a request names.
func (s *Server) warmShareLink(link string, meme accessor.Meme) {
	if s.config.PublicBaseURL == "" || !strings.HasPrefix(link, s.config.PublicBaseURL+"/s/") {
		return
	}
	if meme.SizeBytes > cloudflareMaxCacheableBytes {
		return
	}
	if !s.shareWarm.begin(link) {
		return
	}
	started := s.background.Go(func(ctx context.Context) {
		defer s.shareWarm.end(link)
		s.shareWarm.fetch(ctx, link)
	})
	if !started {
		s.shareWarm.end(link)
	}
}

// fetch requests link and logs what Cloudflare did with it. Log lines carry
// no URL: request logs never show share link codes, and neither may these.
func (w *shareCacheWarmer) fetch(ctx context.Context, link string) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		log.Printf("share link cache warm-up failed: %v", withoutRequestURL(err))
		return
	}
	request.Header.Set("User-Agent", "MemeIndex share link cache warm-up")
	response, err := w.httpClient().Do(request)
	if err != nil {
		log.Printf("share link cache warm-up failed: %v", withoutRequestURL(err))
		return
	}
	defer func() { _ = response.Body.Close() }()

	cacheStatus := response.Header.Get("Cf-Cache-Status")
	ray := response.Header.Get("Cf-Ray")
	colo := ray[strings.LastIndex(ray, "-")+1:]
	var read int64
	if response.StatusCode == http.StatusOK && !strings.EqualFold(cacheStatus, "HIT") {
		// Read the whole file so Cloudflare's copy is complete, rather than
		// relying on it to finish a fetch this client walked away from. A HIT
		// is already cached and needs no second download.
		read, err = io.Copy(io.Discard, response.Body)
	}
	if err != nil {
		log.Printf("share link cache warm-up failed reading the file after %d bytes: %v", read, withoutRequestURL(err))
		return
	}
	log.Printf("share link cache warm-up: status %d, cf-cache-status=%s colo=%s, %d bytes read",
		response.StatusCode, sanitizeLogField(orDash(cacheStatus)), sanitizeLogField(orDash(colo)), read)
}

// withoutRequestURL drops the URL net/http wraps a request error in, which
// here would put a share link code in the log.
func withoutRequestURL(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err
	}
	return err
}

func orDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}
