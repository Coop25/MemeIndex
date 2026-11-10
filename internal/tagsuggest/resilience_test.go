package tagsuggest

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestExtractTagsFromReplyCutOffInsideTagsArray(t *testing.T) {
	tags, err := extractTagsFromModelText(`{"tags":["office humor","monday","coffee","kitc`)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if want := []string{"office humor", "monday", "coffee"}; !slices.Equal(tags, want) {
		t.Fatalf("tags = %v, want %v", tags, want)
	}
}

func TestExtractTagsFromReplyCutOffInsideText(t *testing.T) {
	tags, err := extractTagsFromModelText(`{"tags":["cat","kitchen"],"text":"when you hear the can open and`)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if want := []string{"cat", "kitchen"}; !slices.Equal(tags, want) {
		t.Fatalf("tags = %v, want %v", tags, want)
	}
}

func TestTagsFromTruncatedJSONIgnoresOtherShapes(t *testing.T) {
	for _, raw := range []string{``, `no json here`, `{"text":"tags"`, `{"tags":{"a":"b"`, `{"other":["x","y"`} {
		if tags := tagsFromTruncatedJSON(raw); len(tags) != 0 {
			t.Errorf("tagsFromTruncatedJSON(%q) = %v, want none", raw, tags)
		}
	}
}

func testAsset(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "asset.jpg")
	if err := os.WriteFile(path, []byte("fake image data"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSlowModelIsReportedAsTimeoutAndNotAskedTwice(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		<-release
	}))
	defer server.Close()
	defer close(release)

	service := New(Config{OllamaURL: server.URL, Model: "test-vision", Timeout: 100 * time.Millisecond})
	_, err := service.Suggest(context.Background(), Request{AssetPaths: []string{testAsset(t)}})
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v, want ErrTimeout", err)
	}
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, should also be ErrUnavailable", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("model was asked %d times, want 1 (no fallback after a timeout)", got)
	}
}

func TestRefusedConnectionIsUnavailableButNotTimeout(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	url := server.URL
	server.Close()

	service := New(Config{OllamaURL: url, Model: "test-vision", Timeout: time.Second})
	_, err := service.Suggest(context.Background(), Request{AssetPaths: []string{testAsset(t)}})
	if !errors.Is(err, ErrUnavailable) || errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v, want ErrUnavailable and not ErrTimeout", err)
	}
}

func TestUnloadAsksOllamaToDropTheModel(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			t.Errorf("unexpected endpoint %s", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"done":true,"done_reason":"unload"}`))
	}))
	defer server.Close()

	service := New(Config{OllamaURL: server.URL, Model: "test-vision"})
	if err := service.Unload(context.Background()); err != nil {
		t.Fatalf("unload: %v", err)
	}
	if got["model"] != "test-vision" || got["keep_alive"] != float64(0) {
		t.Fatalf("unload request = %#v", got)
	}
	if _, hasPrompt := got["prompt"]; hasPrompt {
		t.Fatalf("unload request must not carry a prompt: %#v", got)
	}
}

func TestPromptAsksForSceneTagsAndAllowsThem(t *testing.T) {
	prompt := buildPrompt(Request{Filename: "a.jpg"}, nil, nil, 8)
	for _, want := range []string{"setting or location", "conditions", "field named text"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt is missing %q", want)
		}
	}
	service := New(Config{OllamaURL: "http://ollama.invalid", Model: "m"})
	result := service.buildResult(Request{}, []string{"outdoors", "beach", "room", "background", "sunset"}, "")
	if want := []string{"outdoors", "beach", "sunset"}; !slices.Equal(result.Tags, want) {
		t.Fatalf("tags = %v, want %v", result.Tags, want)
	}
}
