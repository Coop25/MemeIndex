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

type shareWarmTransport func(*http.Request) (*http.Response, error)

func (f shareWarmTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// recordShareWarms points the server's warm-up client at a fake Cloudflare
// that answers every request with respond, and returns the URLs it was asked
// for once background work has finished.
func recordShareWarms(t *testing.T, server *Server, respond func(*http.Request) (*http.Response, error)) func() []string {
	t.Helper()
	var mu sync.Mutex
	var fetched []string
	server.shareWarm.client = &http.Client{Transport: shareWarmTransport(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		fetched = append(fetched, r.URL.String())
		mu.Unlock()
		return respond(r)
	})}
	return func() []string {
		t.Helper()
		if !server.background.Stop(5 * time.Second) {
			t.Fatal("share warm-up still running")
		}
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), fetched...)
	}
}

func cloudflareMiss(r *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Cf-Cache-Status": {"MISS"}, "Cf-Ray": {"8f1c2a3b4c5d6e7f-ORD"}},
		Body:       io.NopCloser(strings.NewReader("test image bytes")),
		Request:    r,
	}, nil
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

func TestCreatingAShareWarmsItsLinkThroughThePublicOrigin(t *testing.T) {
	server, _, meme := newShareTestServer(t)
	server.config.PublicBaseURL = "https://memes.example.com"
	fetched := recordShareWarms(t, server, cloudflareMiss)

	link := createShareLink(t, server, meme)
	if got := fetched(); len(got) != 1 || got[0] != link {
		t.Fatalf("warmed %q, want exactly the new link %q", got, link)
	}
}

// Without a configured public origin the link is built from the request's own
// Host header, which the server must never be made to fetch.
func TestShareWarmingNeedsAConfiguredPublicOrigin(t *testing.T) {
	server, _, meme := newShareTestServer(t)
	fetched := recordShareWarms(t, server, cloudflareMiss)

	createShareLink(t, server, meme)
	if got := fetched(); len(got) != 0 {
		t.Fatalf("warmed %q with no public origin configured", got)
	}
}

func TestShareWarmingSkipsLinksCloudflareWillNotCache(t *testing.T) {
	server, _, meme := newShareTestServer(t)
	server.config.PublicBaseURL = "https://memes.example.com"
	fetched := recordShareWarms(t, server, cloudflareMiss)

	huge := meme
	huge.SizeBytes = 600 << 20
	server.warmShareLink("https://memes.example.com/s/7c9e6679-7425-40de-944b-e07fc1f90ae7.mp4", huge)
	server.warmShareLink("https://memes.example.com/m/"+meme.ID+"?share=token", meme)
	server.warmShareLink("https://elsewhere.example.com/s/7c9e6679-7425-40de-944b-e07fc1f90ae7.png", meme)
	if got := fetched(); len(got) != 0 {
		t.Fatalf("warmed %q", got)
	}
}

// Pressing Share again while a warm-up is still downloading must not start a
// second download of the same file.
func TestShareWarmingRunsOncePerLinkAtATime(t *testing.T) {
	server, _, meme := newShareTestServer(t)
	server.config.PublicBaseURL = "https://memes.example.com"
	release := make(chan struct{})
	started := make(chan struct{}, 4)
	fetched := recordShareWarms(t, server, func(r *http.Request) (*http.Response, error) {
		started <- struct{}{}
		<-release
		return cloudflareMiss(r)
	})

	link := "https://memes.example.com/s/7c9e6679-7425-40de-944b-e07fc1f90ae7.png"
	server.warmShareLink(link, meme)
	<-started
	server.warmShareLink(link, meme)
	close(release)
	if got := fetched(); len(got) != 1 {
		t.Fatalf("warmed %d times while the first was running, want 1", len(got))
	}
}

// The container's HTTP(S)_PROXY points at the downloader's egress allowlist,
// which does not include this site.
func TestShareWarmClientGoesStraightToCloudflare(t *testing.T) {
	client := newShareWarmClient()
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.Proxy != nil {
		t.Fatal("share warm-up client uses the environment's proxy")
	}
	request := httptest.NewRequest(http.MethodGet, "https://memes.example.com/m/abc", nil)
	if err := client.CheckRedirect(request, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("share warm-up follows redirects: %v", err)
	}
}

// Request logs never show share link codes; the warm-up's own log lines
// must not either, even when the request fails.
func TestShareWarmingLogsNoShareCode(t *testing.T) {
	var logs bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(previous) })

	const code = "7c9e6679-7425-40de-944b-e07fc1f90ae7"
	link := "https://memes.example.com/s/" + code + ".png"
	for _, respond := range []func(*http.Request) (*http.Response, error){
		cloudflareMiss,
		func(*http.Request) (*http.Response, error) { return nil, errors.New("connection refused") },
	} {
		server, _, meme := newShareTestServer(t)
		server.config.PublicBaseURL = "https://memes.example.com"
		fetched := recordShareWarms(t, server, respond)
		server.warmShareLink(link, meme)
		fetched()
	}
	output := logs.String()
	if strings.Contains(output, code) {
		t.Fatalf("share link code reached the log:\n%s", output)
	}
	for _, want := range []string{"cf-cache-status=MISS", "colo=ORD", "connection refused"} {
		if !strings.Contains(output, want) {
			t.Fatalf("log lacks %q:\n%s", want, output)
		}
	}
}
