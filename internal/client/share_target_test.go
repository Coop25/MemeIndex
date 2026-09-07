package client

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"memeindex/internal/accessor"
	"memeindex/internal/manager"
)

func newShareTargetTestServer(t *testing.T) (*Server, *accessor.MemeStore) {
	t.Helper()
	store, err := accessor.NewMemeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &Server{managers: manager.NewMemeManager(store)}, store
}

func shareTargetRequest(t *testing.T, fields map[string]string, fileField, fileName string, fileBody []byte) *http.Request {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	if fileField != "" {
		part, err := writer.CreateFormFile(fileField, fileName)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(fileBody); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "https://memes.example.com/share-target", body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
}

func TestHandleShareTargetImportsSharedFile(t *testing.T) {
	server, store := newShareTargetTestServer(t)

	request := shareTargetRequest(t, map[string]string{"title": "Cat", "text": "a great cat"}, "files", "cat.png", []byte("shared png bytes"))
	recorder := httptest.NewRecorder()
	server.handleShareTarget(recorder, request)

	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if location := recorder.Header().Get("Location"); location != "/?shared=ok&n=1" {
		t.Fatalf("Location = %q", location)
	}
	memes := store.List("", "", false, "")
	if len(memes) != 1 {
		t.Fatalf("expected 1 stored meme, got %d", len(memes))
	}
	if memes[0].Notes != "Cat\na great cat" {
		t.Fatalf("notes = %q", memes[0].Notes)
	}
}

func TestHandleShareTargetDeduplicatesSharedFile(t *testing.T) {
	server, _ := newShareTargetTestServer(t)
	payload := []byte("identical shared bytes")

	first := httptest.NewRecorder()
	server.handleShareTarget(first, shareTargetRequest(t, nil, "files", "a.png", payload))
	if got := first.Header().Get("Location"); got != "/?shared=ok&n=1" {
		t.Fatalf("first Location = %q (status %d)", got, first.Code)
	}

	second := httptest.NewRecorder()
	server.handleShareTarget(second, shareTargetRequest(t, nil, "files", "b.png", payload))
	if got := second.Header().Get("Location"); got != "/?shared=dup" {
		t.Fatalf("second Location = %q (status %d)", got, second.Code)
	}
}

func TestHandleShareTargetEmptyShareRedirects(t *testing.T) {
	server, _ := newShareTargetTestServer(t)
	recorder := httptest.NewRecorder()
	server.handleShareTarget(recorder, shareTargetRequest(t, map[string]string{"text": "no link here"}, "", "", nil))
	if got := recorder.Header().Get("Location"); got != "/?shared=empty" {
		t.Fatalf("Location = %q (status %d)", got, recorder.Code)
	}
}

func TestHandleShareTargetGatesOnViewPermission(t *testing.T) {
	server, store := newShareTargetTestServer(t)
	server.auth = &authService{}

	anon := httptest.NewRecorder()
	server.handleShareTarget(anon, shareTargetRequest(t, nil, "files", "a.png", []byte("x")))
	if got := anon.Header().Get("Location"); got != "/?shared=forbidden" {
		t.Fatalf("no session: Location = %q (status %d)", got, anon.Code)
	}

	noView := httptest.NewRecorder()
	noViewReq := shareTargetRequest(t, nil, "files", "a.png", []byte("x"))
	noViewReq = noViewReq.WithContext(contextWithSession(noViewReq.Context(), authSession{Permissions: authPermissions{}}))
	server.handleShareTarget(noView, noViewReq)
	if got := noView.Header().Get("Location"); got != "/?shared=forbidden" {
		t.Fatalf("view-less session: Location = %q", got)
	}

	viewer := httptest.NewRecorder()
	viewerReq := shareTargetRequest(t, nil, "files", "shared.png", []byte("viewer shared bytes"))
	viewerReq = viewerReq.WithContext(contextWithSession(viewerReq.Context(), authSession{Permissions: authPermissions{CanView: true}}))
	server.handleShareTarget(viewer, viewerReq)
	if got := viewer.Header().Get("Location"); got != "/?shared=ok&n=1" {
		t.Fatalf("viewer session: Location = %q (status %d)", got, viewer.Code)
	}
	if got := len(store.List("", "", false, "")); got != 1 {
		t.Fatalf("expected 1 stored meme for a view-permission user, got %d", got)
	}
}

func TestHandleShareTargetGetRedirectsToApp(t *testing.T) {
	server, _ := newShareTargetTestServer(t)
	recorder := httptest.NewRecorder()
	server.handleShareTarget(recorder, httptest.NewRequest(http.MethodGet, "https://memes.example.com/share-target", nil))
	if recorder.Code != http.StatusSeeOther || recorder.Header().Get("Location") != "/" {
		t.Fatalf("status = %d, Location = %q", recorder.Code, recorder.Header().Get("Location"))
	}
}

func TestManifestDeclaresFileShareTarget(t *testing.T) {
	manifestBytes, err := os.ReadFile(filepath.Join("..", "..", "static", "manifest.webmanifest"))
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	if err := os.Mkdir("static", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("static", "manifest.webmanifest"), manifestBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	server := &Server{}
	recorder := httptest.NewRecorder()
	server.handleWebManifest(recorder, httptest.NewRequest(http.MethodGet, "https://memes.example.com/manifest.webmanifest", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}

	var manifest struct {
		ShareTarget struct {
			Action  string `json:"action"`
			Method  string `json:"method"`
			Enctype string `json:"enctype"`
			Params  struct {
				Files []struct {
					Name   string   `json:"name"`
					Accept []string `json:"accept"`
				} `json:"files"`
			} `json:"params"`
		} `json:"share_target"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &manifest); err != nil {
		t.Fatalf("manifest is not valid JSON: %v", err)
	}
	st := manifest.ShareTarget
	if st.Action != "/share-target" {
		t.Fatalf("share_target.action = %q", st.Action)
	}
	if !strings.EqualFold(st.Method, http.MethodPost) {
		t.Fatalf("share_target.method = %q", st.Method)
	}
	if st.Enctype != "multipart/form-data" {
		t.Fatalf("share_target.enctype = %q", st.Enctype)
	}
	if len(st.Params.Files) != 1 || st.Params.Files[0].Name != "files" || len(st.Params.Files[0].Accept) == 0 {
		t.Fatalf("share_target.params.files = %+v", st.Params.Files)
	}
}

func TestFirstSharedURL(t *testing.T) {
	cases := []struct {
		name   string
		inputs []string
		want   string
	}{
		{"dedicated url field", []string{"https://example.com/a.png", ""}, "https://example.com/a.png"},
		{"url embedded in text", []string{"", "look at this https://example.com/b.mp4 lol"}, "https://example.com/b.mp4"},
		{"trailing punctuation trimmed", []string{"", "(https://example.com/c.gif)."}, "https://example.com/c.gif"},
		{"prefers first field", []string{"https://a.example/1", "https://b.example/2"}, "https://a.example/1"},
		{"rejects non-http scheme", []string{"ftp://example.com/x", "javascript:alert(1)"}, ""},
		{"rejects credentials in url", []string{"https://user:pass@example.com/x"}, ""},
		{"no url", []string{"just some words", ""}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := firstSharedURL(tc.inputs...); got != tc.want {
				t.Fatalf("firstSharedURL(%q) = %q, want %q", tc.inputs, got, tc.want)
			}
		})
	}
}

func TestSharedNotes(t *testing.T) {
	cases := []struct {
		name        string
		title, text string
		want        string
	}{
		{"both kept", "Title", "Body text", "Title\nBody text"},
		{"title only", "Title", "", "Title"},
		{"text only", "", "Body", "Body"},
		{"title contained in text is dropped", "cat", "cat picture", "cat picture"},
		{"empty", "  ", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sharedNotes(tc.title, tc.text); got != tc.want {
				t.Fatalf("sharedNotes(%q, %q) = %q, want %q", tc.title, tc.text, got, tc.want)
			}
		})
	}
}
