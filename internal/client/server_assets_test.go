package client

import (
	"html"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestIndexServesFreshContentVersionedAssets(t *testing.T) {
	// Use the real template to catch conflicts between hardcoded and server versions.
	template, err := os.ReadFile(filepath.Join("..", "..", "static", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	if err := os.Mkdir("static", 0755); err != nil {
		t.Fatal(err)
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join("static", name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("index.html", string(template))
	write("app.js", "first script")
	write("styles.css", "first stylesheet")
	server := &Server{}
	assetPattern := regexp.MustCompile(`(?:src|href)="(/static/(?:app\.js|styles\.css)[^"]*)"`)
	render := func(target string) map[string]*url.URL {
		t.Helper()
		recorder := httptest.NewRecorder()
		server.handleIndex(recorder, httptest.NewRequest("GET", target, nil))
		if recorder.Code != 200 {
			t.Fatalf("status = %d", recorder.Code)
		}
		if got := recorder.Header().Get("Cache-Control"); got != "no-store, max-age=0" {
			t.Fatalf("index cache policy = %q", got)
		}
		assets := map[string]*url.URL{}
		for _, match := range assetPattern.FindAllStringSubmatch(recorder.Body.String(), -1) {
			raw := html.UnescapeString(match[1])
			if strings.Count(raw, "?") != 1 {
				t.Fatalf("malformed asset URL: %q", raw)
			}
			parsed, err := url.Parse(raw)
			if err != nil {
				t.Fatal(err)
			}
			if parsed.Query().Get("v") != BuildVersion() || len(parsed.Query().Get("h")) != 16 {
				t.Fatalf("missing build version or content hash: %q", raw)
			}
			assets[parsed.Path] = parsed
		}
		if len(assets) != 2 {
			t.Fatalf("found %d JS/CSS references, want 2", len(assets))
		}
		return assets
	}
	first := render("/")
	unchanged := render("/")
	for path, asset := range first {
		if unchanged[path].String() != asset.String() {
			t.Fatalf("unchanged file %s received a different asset URL", path)
		}
	}
	write("app.js", "updated script")
	write("styles.css", "updated stylesheet")
	updated := render("/")
	for path, asset := range first {
		if updated[path].Query().Get("h") == asset.Query().Get("h") {
			t.Fatalf("changed file %s retained its old cache key", path)
		}
	}
	refreshed := render("/?refresh=" + url.QueryEscape("test & reload"))
	for path, asset := range refreshed {
		if asset.Query().Get("r") != "test & reload" || asset.Query().Get("h") != updated[path].Query().Get("h") {
			t.Fatalf("refresh token broke the asset URL: %s", asset)
		}
	}
}
