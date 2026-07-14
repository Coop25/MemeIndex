package tagsuggest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestExtractTagsFromModelText(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{name: "object", raw: `{"tags":["work","office humor"]}`, want: []string{"work", "office humor"}},
		{name: "fenced", raw: "```json\n{\"tags\":[\"work\",\"boss\"]}\n```", want: []string{"work", "boss"}},
		{name: "prose around JSON", raw: `Here you go: {"tags":["dating","awkward"]} done`, want: []string{"dating", "awkward"}},
		{name: "array", raw: `["gaming","failure"]`, want: []string{"gaming", "failure"}},
		{name: "explicit plain list", raw: `Tags: work, office humor`, want: []string{"work", "office humor"}},
		{name: "bullets", raw: "- work\n- office humor", want: []string{"work", "office humor"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := extractTagsFromModelText(test.raw)
			if err != nil {
				t.Fatalf("extract tags: %v", err)
			}
			got = normalizeTags(got)
			if len(got) != len(test.want) {
				t.Fatalf("got %v, want %v", got, test.want)
			}
			for index := range test.want {
				if got[index] != test.want[index] {
					t.Fatalf("got %v, want %v", got, test.want)
				}
			}
		})
	}
}

func TestExtractTagsRejectsJSONFragmentsAndExplanation(t *testing.T) {
	for _, raw := range []string{
		`{"tags": "work, office"}`,
		`{"tags":[{"name":"work"}]}`,
		`Here are the suggested tags because the image looks funny.`,
		`"tags": "work"`,
	} {
		if tags, err := extractTagsFromModelText(raw); err == nil {
			t.Fatalf("expected %q to fail, got %v", raw, tags)
		}
	}
}

func TestGenerateEndpointUsesSchemaAndCleansResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			t.Fatalf("unexpected endpoint %s", r.URL.Path)
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		format, ok := request["format"].(map[string]any)
		if !ok || format["type"] != "object" {
			t.Fatalf("generate request did not include object schema: %#v", request["format"])
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"response": "```json\n{\"tags\":[\"work\",\"office humor\",\"image\"]}\n```",
		})
	}))
	defer server.Close()

	asset, err := os.CreateTemp(t.TempDir(), "asset-*.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := asset.Write([]byte("fake image data")); err != nil {
		t.Fatal(err)
	}
	if err := asset.Close(); err != nil {
		t.Fatal(err)
	}

	service := New(Config{
		OllamaURL:    server.URL,
		Model:        "test-vision",
		Timeout:      2 * time.Second,
		MaxTags:      8,
		GenerateOnly: true,
	})
	result, err := service.Suggest(context.Background(), Request{
		AssetPaths:  []string{asset.Name()},
		Filename:    "meeting.jpg",
		ContentType: "image/jpeg",
	})
	if err != nil {
		t.Fatalf("suggest: %v", err)
	}
	if len(result.Tags) != 2 || result.Tags[0] != "work" || result.Tags[1] != "office humor" {
		t.Fatalf("unexpected tags %v", result.Tags)
	}
}

func TestPromptKnownTagsIsCapped(t *testing.T) {
	tags := make([]string, 100)
	for index := range tags {
		tags[index] = "tag-" + string(rune('a'+index%26)) + string(rune('0'+index/26))
	}
	if got := promptKnownTags(tags); len(got) != 60 {
		t.Fatalf("got %d known tags, want 60", len(got))
	}
}

func TestBuildResultDoesNotInventCompanionTags(t *testing.T) {
	service := &Service{model: "test", maxTags: 8}
	result := service.buildResult(Request{}, []string{"trans", "video games", "image processing", "image"})
	want := []string{"trans", "video games", "image processing"}
	if len(result.Tags) != len(want) {
		t.Fatalf("got %v, want %v", result.Tags, want)
	}
	for index := range want {
		if result.Tags[index] != want[index] {
			t.Fatalf("got %v, want %v", result.Tags, want)
		}
	}
}
