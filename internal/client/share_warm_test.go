package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"memeindex/internal/accessor"
)

const (
	testWebhookToken = "a-secret-webhook-token_123"
	testWebhookURL   = "https://discord.com/api/webhooks/123456789012345678/" + testWebhookToken
	testShareCode    = "7c9e6679-7425-40de-944b-e07fc1f90ae7"
	testShareLink    = "https://memes.example.com/s/" + testShareCode + ".png"
)

type discordTransport func(*http.Request) (*http.Response, error)

func (f discordTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func discordJSON(r *http.Request, status int, body string) (*http.Response, error) {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    r,
	}, nil
}

// fakeDiscord stands in for Discord's webhook API. It records every request
// and lets each test decide how Discord answers.
type fakeDiscord struct {
	t       *testing.T
	mu      sync.Mutex
	posts   []string // content of each message posted
	polls   int
	respond func(r *http.Request, call int) (*http.Response, error)
	calls   int
}

// previewAfterPolls answers like Discord does: the message is created with no
// embeds, and its preview shows up once Discord has fetched the link.
func previewAfterPolls(polls int) func(*http.Request, int) (*http.Response, error) {
	seen := 0
	return func(r *http.Request, _ int) (*http.Response, error) {
		if r.Method == http.MethodPost {
			return discordJSON(r, http.StatusOK, `{"id":"555","embeds":[]}`)
		}
		seen++
		if seen < polls {
			return discordJSON(r, http.StatusOK, `{"id":"555","embeds":[]}`)
		}
		return discordJSON(r, http.StatusOK, `{"id":"555","embeds":[{"type":"video"}]}`)
	}
}

func newFakeDiscord(t *testing.T, server *Server, respond func(*http.Request, int) (*http.Response, error)) *fakeDiscord {
	t.Helper()
	fake := &fakeDiscord{t: t, respond: respond}
	server.config.PublicBaseURL = "https://memes.example.com"
	server.config.DiscordWarmupWebhookURL = testWebhookURL
	server.shareWarm.pollInterval = time.Millisecond
	server.shareWarm.client = &http.Client{Transport: discordTransport(func(r *http.Request) (*http.Response, error) {
		fake.mu.Lock()
		fake.calls++
		call := fake.calls
		switch {
		case r.Method == http.MethodPost && r.URL.String() == testWebhookURL+"?wait=true":
			var payload struct {
				Content         string `json:"content"`
				AllowedMentions struct {
					Parse []string `json:"parse"`
				} `json:"allowed_mentions"`
			}
			body, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(body, &payload); err != nil || payload.AllowedMentions.Parse == nil || len(payload.AllowedMentions.Parse) != 0 {
				fake.mu.Unlock()
				t.Errorf("webhook post %s must carry the link and allow no mentions", body)
				return discordJSON(r, http.StatusBadRequest, `{}`)
			}
			fake.posts = append(fake.posts, payload.Content)
		case r.Method == http.MethodGet && r.URL.String() == testWebhookURL+"/messages/555":
			fake.polls++
		default:
			fake.mu.Unlock()
			t.Errorf("unexpected Discord request %s %s", r.Method, r.URL)
			return discordJSON(r, http.StatusNotFound, `{}`)
		}
		fake.mu.Unlock()
		return fake.respond(r, call)
	})}
	return fake
}

// wait lets background work finish and returns the messages posted.
func (f *fakeDiscord) wait(server *Server) []string {
	f.t.Helper()
	if !server.background.Stop(5 * time.Second) {
		f.t.Fatal("Discord warm-up still running")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.posts...)
}

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var logs bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(previous) })
	return &logs
}

func createShareLink(t *testing.T, server *Server, meme accessor.Meme) string {
	t.Helper()
	recorder := httptest.NewRecorder()
	server.createMemeShare(recorder, httptest.NewRequest(http.MethodPost, "https://memes.example.com/api/memes/"+meme.ID+"/share", nil), meme.ID)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	return payload.URL
}

func TestCreatingASharePostsItsLinkToTheDiscordWarmupWebhook(t *testing.T) {
	logs := captureLog(t)
	server, _, meme := newShareTestServer(t)
	fake := newFakeDiscord(t, server, previewAfterPolls(3))

	link := createShareLink(t, server, meme)
	if posts := fake.wait(server); len(posts) != 1 || posts[0] != link {
		t.Fatalf("posted %q, want exactly the new link %q", posts, link)
	}
	if fake.polls != 3 {
		t.Fatalf("read the message back %d times, want until the preview appeared (3)", fake.polls)
	}
	if output := logs.String(); !strings.Contains(output, "Discord warm-up: preview ready after") || !strings.Contains(output, "(video)") {
		t.Fatalf("log does not say when Discord finished:\n%s", output)
	}
}

// Without a webhook nothing is warmed; without a public origin the link is
// built from the request's own Host header and must not be posted anywhere.
func TestDiscordWarmupNeedsAWebhookAndAPublicOrigin(t *testing.T) {
	for _, setup := range []func(*Server){
		func(s *Server) { s.config.DiscordWarmupWebhookURL = "" },
		func(s *Server) { s.config.PublicBaseURL = "" },
	} {
		server, _, meme := newShareTestServer(t)
		fake := newFakeDiscord(t, server, previewAfterPolls(1))
		setup(server)
		createShareLink(t, server, meme)
		if posts := fake.wait(server); len(posts) != 0 {
			t.Fatalf("posted %q", posts)
		}
	}
}

func TestDiscordWarmupSkipsLinksCloudflareWillNotCache(t *testing.T) {
	server, _, meme := newShareTestServer(t)
	fake := newFakeDiscord(t, server, previewAfterPolls(1))

	huge := meme
	huge.SizeBytes = 600 << 20
	server.warmShareLink(testShareLink, huge)
	server.warmShareLink("https://memes.example.com/m/"+meme.ID+"?share=token", meme)
	server.warmShareLink("https://elsewhere.example.com/s/"+testShareCode+".png", meme)
	if posts := fake.wait(server); len(posts) != 0 {
		t.Fatalf("posted %q", posts)
	}
}

// Pressing Share again renews the same link. It must not post the link again
// while the first post is in flight, nor while Cloudflare still holds the file.
func TestDiscordWarmupPostsEachLinkOncePerCacheLifetime(t *testing.T) {
	server, _, meme := newShareTestServer(t)
	release := make(chan struct{})
	posted := make(chan struct{}, 4)
	preview := previewAfterPolls(1)
	fake := newFakeDiscord(t, server, func(r *http.Request, call int) (*http.Response, error) {
		if r.Method == http.MethodPost {
			posted <- struct{}{}
			<-release
		}
		return preview(r, call)
	})

	server.warmShareLink(testShareLink, meme)
	<-posted
	server.warmShareLink(testShareLink, meme)
	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for {
		server.shareWarm.mu.Lock()
		_, running := server.shareWarm.inFlight[testShareLink]
		server.shareWarm.mu.Unlock()
		if !running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("first warm-up never finished")
		}
		time.Sleep(time.Millisecond)
	}
	server.warmShareLink(testShareLink, meme)
	if posts := fake.wait(server); len(posts) != 1 {
		t.Fatalf("posted the link %d times, want once", len(posts))
	}
}

func TestDiscordWarmupWaitsOutRateLimits(t *testing.T) {
	logs := captureLog(t)
	server, _, meme := newShareTestServer(t)
	preview := previewAfterPolls(1)
	limited := false
	fake := newFakeDiscord(t, server, func(r *http.Request, call int) (*http.Response, error) {
		if r.Method == http.MethodPost && !limited {
			limited = true
			return discordJSON(r, http.StatusTooManyRequests, `{"message":"You are being rate limited.","retry_after":0.001}`)
		}
		return preview(r, call)
	})

	server.warmShareLink(testShareLink, meme)
	if posts := fake.wait(server); len(posts) != 2 {
		t.Fatalf("posted %d times, want a retry after the rate limit", len(posts))
	}
	if !strings.Contains(logs.String(), "preview ready") {
		t.Fatalf("warm-up did not finish after the rate limit:\n%s", logs.String())
	}
}

func TestDiscordWarmupGivesUpWhenNoPreviewArrives(t *testing.T) {
	logs := captureLog(t)
	server, _, meme := newShareTestServer(t)
	fake := newFakeDiscord(t, server, previewAfterPolls(1_000_000))
	server.shareWarm.previewTimeout = 20 * time.Millisecond

	server.warmShareLink(testShareLink, meme)
	fake.wait(server)
	if !strings.Contains(logs.String(), "no preview from Discord after") {
		t.Fatalf("log does not report the missing preview:\n%s", logs.String())
	}
}

// The container's HTTP(S)_PROXY points at the downloader's egress allowlist,
// which does not include Discord.
func TestDiscordWebhookClientGoesStraightToDiscord(t *testing.T) {
	client := newDiscordWebhookClient()
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.Proxy != nil {
		t.Fatal("Discord webhook client uses the environment's proxy")
	}
	request := httptest.NewRequest(http.MethodGet, testWebhookURL, nil)
	if err := client.CheckRedirect(request, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("Discord webhook client follows redirects: %v", err)
	}
}

// Request logs never show share link codes, and the webhook URL carries the
// token that lets anyone post to the channel. Neither may reach the log.
func TestDiscordWarmupLogsNeitherShareCodeNorWebhookToken(t *testing.T) {
	logs := captureLog(t)
	for _, respond := range []func(*http.Request, int) (*http.Response, error){
		previewAfterPolls(1),
		func(r *http.Request, _ int) (*http.Response, error) { return nil, errors.New("connection refused") },
		func(r *http.Request, _ int) (*http.Response, error) {
			return discordJSON(r, http.StatusNotFound, `{"message":"Unknown Webhook","code":10015}`)
		},
	} {
		server, _, meme := newShareTestServer(t)
		fake := newFakeDiscord(t, server, respond)
		server.warmShareLink(testShareLink, meme)
		fake.wait(server)
	}
	output := logs.String()
	for _, secret := range []string{testShareCode, testWebhookToken} {
		if strings.Contains(output, secret) {
			t.Fatalf("%q reached the log:\n%s", secret, output)
		}
	}
	for _, want := range []string{"preview ready", "connection refused", "404", "Unknown Webhook"} {
		if !strings.Contains(output, want) {
			t.Fatalf("log lacks %q:\n%s", want, output)
		}
	}
}

func TestDiscordWarmupWebhookSettingIsValidated(t *testing.T) {
	good := Config{MaxUploadBytes: 1024, AllowAnonymous: true, PublicBaseURL: "https://memes.example.com", DiscordWarmupWebhookURL: testWebhookURL}
	if err := good.validateSecurity(); err != nil {
		t.Fatal(err)
	}
	for _, webhook := range []string{
		"https://discordapp.com/api/webhooks/123456789012345678/" + testWebhookToken,
		"https://discord.com/api/v10/webhooks/123456789012345678/" + testWebhookToken,
	} {
		ok := good
		ok.DiscordWarmupWebhookURL = webhook
		if err := ok.validateSecurity(); err != nil {
			t.Errorf("%s rejected: %v", webhook, err)
		}
	}
	for label, bad := range map[string]Config{
		"plain http":       {DiscordWarmupWebhookURL: "http://discord.com/api/webhooks/123456789012345678/" + testWebhookToken},
		"other host":       {DiscordWarmupWebhookURL: "https://example.com/api/webhooks/123456789012345678/" + testWebhookToken},
		"not a webhook":    {DiscordWarmupWebhookURL: "https://discord.com/api/channels/123456789012345678/" + testWebhookToken},
		"query string":     {DiscordWarmupWebhookURL: testWebhookURL + "?thread_id=1"},
		"no public origin": {DiscordWarmupWebhookURL: testWebhookURL, PublicBaseURL: "-"},
	} {
		config := good
		config.DiscordWarmupWebhookURL = bad.DiscordWarmupWebhookURL
		if bad.PublicBaseURL == "-" {
			config.PublicBaseURL = ""
		}
		err := config.validateSecurity()
		if err == nil {
			t.Errorf("%s: accepted", label)
			continue
		}
		if strings.Contains(err.Error(), testWebhookToken) {
			t.Errorf("%s: error message repeats the webhook token: %v", label, err)
		}
	}
}
