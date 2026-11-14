package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"memeindex/internal/accessor"
)

// When MEMEINDEX_DISCORD_WARMUP_WEBHOOK_URL is set, every new share link is
// posted once to that webhook's channel, which is meant to be a private one.
// Discord then fetches the link itself, through the Cloudflare data center its
// own servers reach, so the file is cached there (and, going by Discord's
// undocumented behaviour, in Discord's own media cache) before the link is
// pasted anywhere for real.
//
// This server cannot see that happen: Cloudflare answers cache hits without
// it. So the warmer reads the message back until Discord has attached its
// preview, which is Discord's own word that it fetched the file, and logs how
// long that took.

const (
	// shareWarmMaxConcurrent caps warm-ups in flight, so a burst of shares
	// does not saturate the server's upload.
	shareWarmMaxConcurrent = 2
	// cloudflareMaxCacheableBytes is the largest file Cloudflare caches on the
	// Free, Pro and Business plans; warming anything bigger would upload it
	// for nothing.
	cloudflareMaxCacheableBytes = 512 << 20
	// discordPreviewTimeout is how long to wait for Discord's preview. A
	// file Cloudflare does not have yet comes over the server's own upload.
	discordPreviewTimeout      = 3 * time.Minute
	discordPreviewPollInterval = 3 * time.Second
	discordRequestTimeout      = 30 * time.Second
	// discordMaxRetries and discordMaxRetryWait bound how long one request
	// waits out Discord's rate limits.
	discordMaxRetries   = 3
	discordMaxRetryWait = 30 * time.Second
)

type shareCacheWarmer struct {
	// client sends requests to Discord; nil means newDiscordWebhookClient.
	client *http.Client
	// pollInterval and previewTimeout override the defaults above in tests.
	pollInterval   time.Duration
	previewTimeout time.Duration

	mu       sync.Mutex
	inFlight map[string]struct{}
	// posted holds when each link was last posted, so pressing Share again
	// does not post it again while Cloudflare may still hold the file.
	posted map[string]time.Time
}

func newDiscordWebhookClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// The container's HTTP(S)_PROXY sends traffic through the downloader's
	// egress allowlist, which does not include Discord.
	transport.Proxy = nil
	return &http.Client{
		Transport:     transport,
		Timeout:       discordRequestTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func (w *shareCacheWarmer) httpClient() *http.Client {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.client == nil {
		w.client = newDiscordWebhookClient()
	}
	return w.client
}

// begin claims link for a warm-up. It reports false when that link is already
// being warmed or was posted within the edge cache lifetime, or when too many
// warm-ups are running.
func (w *shareCacheWarmer) begin(link string, now time.Time) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	for postedLink, at := range w.posted {
		if now.Sub(at) >= shareLinkEdgeCacheTTL {
			delete(w.posted, postedLink)
		}
	}
	if _, running := w.inFlight[link]; running || len(w.inFlight) >= shareWarmMaxConcurrent {
		return false
	}
	if _, recent := w.posted[link]; recent {
		return false
	}
	if w.inFlight == nil {
		w.inFlight = map[string]struct{}{}
	}
	w.inFlight[link] = struct{}{}
	return true
}

func (w *shareCacheWarmer) end(link string, posted bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.inFlight, link)
	if posted {
		if w.posted == nil {
			w.posted = map[string]time.Time{}
		}
		w.posted[link] = time.Now()
	}
}

// warmShareLink posts a share link to the Discord warm-up webhook in the
// background. It only posts links on the configured public origin: without
// one, links are built from the request's own Host header.
func (s *Server) warmShareLink(link string, meme accessor.Meme) {
	webhook := s.config.DiscordWarmupWebhookURL
	if webhook == "" || s.config.PublicBaseURL == "" || !strings.HasPrefix(link, s.config.PublicBaseURL+"/s/") {
		return
	}
	if meme.SizeBytes > cloudflareMaxCacheableBytes {
		return
	}
	if !s.shareWarm.begin(link, time.Now()) {
		return
	}
	started := s.background.Go(func(ctx context.Context) {
		posted := s.shareWarm.warm(ctx, webhook, link)
		s.shareWarm.end(link, posted)
	})
	if !started {
		s.shareWarm.end(link, false)
	}
}

type discordMessage struct {
	ID     string `json:"id"`
	Embeds []struct {
		Type string `json:"type"`
	} `json:"embeds"`
}

// warm posts link to the webhook and waits for Discord's preview. It reports
// whether the link was posted. Log lines carry neither the link (request logs
// never show share link codes) nor the webhook URL (it holds the token).
func (w *shareCacheWarmer) warm(ctx context.Context, webhook, link string) bool {
	payload, err := json.Marshal(map[string]any{
		"content": link,
		// The link is the whole message; it must never ping anyone.
		"allowed_mentions": map[string]any{"parse": []string{}},
	})
	if err != nil {
		log.Printf("Discord warm-up failed: %v", err)
		return false
	}
	start := time.Now()
	var message discordMessage
	if err := w.call(ctx, http.MethodPost, webhook+"?wait=true", payload, &message); err != nil {
		log.Printf("Discord warm-up failed posting the link: %v", err)
		return false
	}
	if !isDiscordID(message.ID) {
		log.Printf("Discord warm-up: posted the link, but Discord's reply had no message id")
		return true
	}

	timeout := w.previewTimeout
	if timeout <= 0 {
		timeout = discordPreviewTimeout
	}
	interval := w.pollInterval
	if interval <= 0 {
		interval = discordPreviewPollInterval
	}
	deadline := start.Add(timeout)
	for len(message.Embeds) == 0 {
		if time.Now().After(deadline) {
			log.Printf("Discord warm-up: no preview from Discord after %s", timeout)
			return true
		}
		if !sleepContext(ctx, interval) {
			return true
		}
		if err := w.call(ctx, http.MethodGet, webhook+"/messages/"+message.ID, nil, &message); err != nil {
			log.Printf("Discord warm-up failed reading the message back: %v", err)
			return true
		}
	}
	log.Printf("Discord warm-up: preview ready after %s (%s)",
		time.Since(start).Round(100*time.Millisecond), sanitizeLogField(orDash(message.Embeds[0].Type)))
	return true
}

// call sends one request to Discord, waiting out rate limits, and decodes a
// successful JSON reply into out.
func (w *shareCacheWarmer) call(ctx context.Context, method, target string, payload []byte, out any) error {
	for attempt := 0; ; attempt++ {
		var body io.Reader
		if payload != nil {
			body = bytes.NewReader(payload)
		}
		request, err := http.NewRequestWithContext(ctx, method, target, body)
		if err != nil {
			return withoutRequestURL(err)
		}
		if payload != nil {
			request.Header.Set("Content-Type", "application/json")
		}
		request.Header.Set("User-Agent", "MemeIndex share link warm-up")
		response, err := w.httpClient().Do(request)
		if err != nil {
			return withoutRequestURL(err)
		}
		data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		_ = response.Body.Close()
		if err != nil {
			return withoutRequestURL(err)
		}
		if response.StatusCode == http.StatusTooManyRequests && attempt < discordMaxRetries {
			if !sleepContext(ctx, discordRetryAfter(response.Header, data)) {
				return ctx.Err()
			}
			continue
		}
		if response.StatusCode < 200 || response.StatusCode > 299 {
			return fmt.Errorf("discord answered %d: %s", response.StatusCode, discordErrorMessage(data))
		}
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("discord's reply was not understood: %w", err)
		}
		return nil
	}
}

// discordRetryAfter reads how long Discord asked to wait, from the JSON
// body's retry_after or the Retry-After header, both in seconds.
func discordRetryAfter(header http.Header, body []byte) time.Duration {
	var limited struct {
		RetryAfter float64 `json:"retry_after"`
	}
	seconds := 1.0
	if json.Unmarshal(body, &limited) == nil && limited.RetryAfter > 0 {
		seconds = limited.RetryAfter
	} else if value, err := strconv.ParseFloat(header.Get("Retry-After"), 64); err == nil && value > 0 {
		seconds = value
	}
	return min(time.Duration(seconds*float64(time.Second)), discordMaxRetryWait)
}

// discordErrorMessage is the message from a Discord error reply, made safe
// for the log.
func discordErrorMessage(body []byte) string {
	var reply struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &reply) != nil || reply.Message == "" {
		return "-"
	}
	message := reply.Message
	if len(message) > 200 {
		message = strings.ToValidUTF8(message[:200], "")
	}
	return sanitizeLogField(message)
}

func isDiscordID(value string) bool {
	if value == "" || len(value) > 32 {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// sleepContext waits for d, reporting false if ctx ends first.
func sleepContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// withoutRequestURL drops the URL net/http wraps a request error in, which
// here would put a share link code or the webhook token in the log.
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
