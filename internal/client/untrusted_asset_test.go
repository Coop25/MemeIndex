package client

import (
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"memeindex/internal/accessor"
)

// registerTestMIMETypes pins the types these tests rely on, since the MIME
// table is read from the OS (/etc/mime.types, the Windows registry) and
// differs between machines.
func registerTestMIMETypes(t *testing.T) {
	t.Helper()
	for ext, typ := range map[string]string{
		".xht":   "application/xhtml+xml",
		".shtml": "text/html",
		".mp4":   "video/mp4",
		".png":   "image/png",
	} {
		if err := mime.AddExtensionType(ext, typ); err != nil {
			t.Fatalf("register %s: %v", ext, err)
		}
	}
}

const testMarkup = `<?xml version="1.0"?><html xmlns="http://www.w3.org/1999/xhtml"><body><script>alert(document.cookie)</script></body></html>`

// testPNG is enough of a PNG for content sniffing.
var testPNG = "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89"

// uploadsTestServer serves dir the way Routes mounts /uploads/, with auth
// off so the request reaches the file server.
func uploadsTestServer(t *testing.T, files map[string]string) (http.Handler, string) {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	server := &Server{}
	return server.withProtectedAssetAuth(http.StripPrefix("/uploads/", http.FileServer(http.Dir(dir)))), dir
}

func fetchUpload(handler http.Handler, path string, header http.Header) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for key, values := range header {
		req.Header[key] = values
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// Extensions outside the hard-coded list still map to markup through the MIME
// table, and http.FileServer only fills that type in after the scriptable
// check has run - so the check has to see the type itself.
func TestUploadsServeMarkupTypesFromTheMIMETableAsDownloads(t *testing.T) {
	registerTestMIMETypes(t)
	handler, _ := uploadsTestServer(t, map[string]string{
		"0190a0b8-0000-7000-8000-000000000001.xht":   testMarkup,
		"0190a0b8-0000-7000-8000-000000000002.shtml": testMarkup,
		// Not in the MIME table at all: the file server sniffs the bytes.
		"0190a0b8-0000-7000-8000-000000000003.zzq": "<!DOCTYPE html><html><script>alert(1)</script></html>",
	})
	for _, name := range []string{
		"0190a0b8-0000-7000-8000-000000000001.xht",
		"0190a0b8-0000-7000-8000-000000000002.shtml",
		"0190a0b8-0000-7000-8000-000000000003.zzq",
	} {
		rec := fetchUpload(handler, "/uploads/"+name, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", name, rec.Code)
		}
		if got := rec.Header().Get("Content-Type"); got != "application/octet-stream" {
			t.Errorf("%s: Content-Type = %q, want application/octet-stream", name, got)
		}
		if got := rec.Header().Get("Content-Disposition"); got != "attachment" {
			t.Errorf("%s: Content-Disposition = %q, want attachment", name, got)
		}
		if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("%s: X-Content-Type-Options = %q, want nosniff", name, got)
		}
	}
}

// An XSLT stylesheet can transform the page that loads it, so it is code too.
// text/xsl, its older name, has no "xml" in it to give it away.
func TestXSLTTypesAreScriptable(t *testing.T) {
	for _, value := range []string{"text/xsl", "TEXT/XSL; charset=utf-8", " application/xslt+xml ", "application/xslt+xml; charset=utf-8"} {
		if !scriptableContentType(value) {
			t.Errorf("scriptableContentType(%q) = false, want true", value)
		}
	}
	for _, value := range []string{"image/png", "video/mp4", "image/svg+xml", "text/plain", ""} {
		if scriptableContentType(value) {
			t.Errorf("scriptableContentType(%q) = true, want false", value)
		}
	}

	if err := mime.AddExtensionType(".xsl", "text/xsl"); err != nil {
		t.Fatal(err)
	}
	handler, _ := uploadsTestServer(t, map[string]string{
		"0190a0b8-0000-7000-8000-000000000004.xsl": `<xsl:stylesheet version="1.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform"/>`,
	})
	rec := fetchUpload(handler, "/uploads/0190a0b8-0000-7000-8000-000000000004.xsl", nil)
	if got := rec.Header().Get("Content-Type"); got != "application/octet-stream" {
		t.Errorf("xsl upload: Content-Type = %q, want application/octet-stream", got)
	}
	if got := rec.Header().Get("Content-Disposition"); got != "attachment" {
		t.Errorf("xsl upload: Content-Disposition = %q, want attachment", got)
	}
}

// The fix must not turn the media the app actually stores into downloads, or
// break the partial and conditional requests video players and caches send.
func TestUploadsKeepMediaPreviewsRangesAndRevalidation(t *testing.T) {
	registerTestMIMETypes(t)
	video := strings.Repeat("v", 4096)
	handler, dir := uploadsTestServer(t, map[string]string{
		"image.png":  testPNG,
		"clip.mp4":   video,
		"vector.svg": `<svg xmlns="http://www.w3.org/2000/svg"></svg>`,
		"page.xht":   testMarkup,
	})

	png := fetchUpload(handler, "/uploads/image.png", nil)
	if got := png.Header().Get("Content-Type"); got != "image/png" {
		t.Errorf("png: Content-Type = %q, want image/png", got)
	}
	if got := png.Header().Get("Content-Disposition"); got != "" {
		t.Errorf("png: Content-Disposition = %q, want inline (unset) so previews render", got)
	}

	// SVG keeps its image type for <img> previews; opening it directly is
	// still a sandboxed download.
	svg := fetchUpload(handler, "/uploads/vector.svg", nil)
	if got, want := svg.Header().Get("Content-Type"), mime.TypeByExtension(".svg"); got != want || got == "application/octet-stream" {
		t.Errorf("svg: Content-Type = %q, want %q", got, want)
	}
	if got := svg.Header().Get("Content-Disposition"); got != "attachment" {
		t.Errorf("svg: Content-Disposition = %q, want attachment", got)
	}

	ranged := fetchUpload(handler, "/uploads/clip.mp4", http.Header{"Range": {"bytes=100-199"}})
	if ranged.Code != http.StatusPartialContent {
		t.Fatalf("video range: status = %d, want 206", ranged.Code)
	}
	if got := ranged.Header().Get("Content-Type"); got != "video/mp4" {
		t.Errorf("video range: Content-Type = %q, want video/mp4", got)
	}
	if got := ranged.Header().Get("Content-Range"); got != "bytes 100-199/4096" {
		t.Errorf("video range: Content-Range = %q", got)
	}
	if ranged.Body.Len() != 100 {
		t.Errorf("video range: body is %d bytes, want 100", ranged.Body.Len())
	}

	// Each part of a multi-range answer carries the file's type in the body,
	// so that has to be the neutral type too.
	multi := fetchUpload(handler, "/uploads/page.xht", http.Header{"Range": {"bytes=0-9,20-29"}})
	if multi.Code != http.StatusPartialContent {
		t.Fatalf("markup multi-range: status = %d, want 206", multi.Code)
	}
	if body := multi.Body.String(); strings.Contains(body, "xhtml") || !strings.Contains(body, "Content-Type: application/octet-stream") {
		t.Errorf("markup multi-range parts are not typed application/octet-stream:\n%s", body)
	}

	info, err := os.Stat(filepath.Join(dir, "clip.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	notModified := fetchUpload(handler, "/uploads/clip.mp4", http.Header{
		"If-Modified-Since": {info.ModTime().UTC().Add(time.Second).Format(http.TimeFormat)},
	})
	if notModified.Code != http.StatusNotModified {
		t.Fatalf("revalidation: status = %d, want 304", notModified.Code)
	}
	if notModified.Body.Len() != 0 {
		t.Errorf("revalidation: 304 has a %d byte body", notModified.Body.Len())
	}

	missing := fetchUpload(handler, "/uploads/gone.xht", nil)
	if missing.Code != http.StatusNotFound {
		t.Errorf("missing file: status = %d, want 404", missing.Code)
	}
}

// A share link with no stored type falls back to the extension too.
func TestSharedAssetWithoutStoredTypeNeutralizesMarkup(t *testing.T) {
	registerTestMIMETypes(t)
	server, store := newShareTargetTestServer(t)
	for name, body := range map[string]string{"shared.xht": testMarkup, "shared.mp4": "not really a video"} {
		if err := os.WriteFile(filepath.Join(store.UploadDir(), name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	rec := httptest.NewRecorder()
	server.serveSharedMemeAsset(rec, httptest.NewRequest(http.MethodGet, "/s/x", nil), accessor.Meme{StoredName: "shared.xht", OriginalName: "page.xht"}, false, 0)
	if got := rec.Header().Get("Content-Type"); got != "application/octet-stream" {
		t.Errorf("shared markup: Content-Type = %q, want application/octet-stream", got)
	}
	if got := rec.Header().Get("Content-Disposition"); got != "attachment" {
		t.Errorf("shared markup: Content-Disposition = %q, want attachment", got)
	}

	// A shared video still opens in the browser's own player.
	rec = httptest.NewRecorder()
	server.serveSharedMemeAsset(rec, httptest.NewRequest(http.MethodGet, "/s/y", nil), accessor.Meme{StoredName: "shared.mp4", OriginalName: "clip.mp4", ContentType: "video/mp4"}, false, 0)
	if got := rec.Header().Get("Content-Type"); got != "video/mp4" {
		t.Errorf("shared video: Content-Type = %q, want video/mp4", got)
	}
	if got := rec.Header().Get("Content-Security-Policy"); got != sharedMediaContentSecurityPolicy {
		t.Errorf("shared video: CSP = %q, want the media policy", got)
	}
	if got := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "inline") {
		t.Errorf("shared video: Content-Disposition = %q, want inline", got)
	}
	if body, _ := io.ReadAll(rec.Result().Body); string(body) != "not really a video" {
		t.Errorf("shared video: body = %q", body)
	}
}

// The guard also checks a type that is only sniffed on the first write, for a
// handler that never calls WriteHeader itself.
func TestScriptableTypeGuardSniffsImplicitWrites(t *testing.T) {
	rec := httptest.NewRecorder()
	guarded := setUntrustedAssetHeaders(rec, "/uploads/unknown")
	if _, err := io.WriteString(guarded, "<!DOCTYPE html><script>alert(1)</script>"); err != nil {
		t.Fatal(err)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/octet-stream" {
		t.Errorf("Content-Type = %q, want application/octet-stream", got)
	}
	if got := rec.Header().Get("Content-Disposition"); got != "attachment" {
		t.Errorf("Content-Disposition = %q, want attachment", got)
	}

	rec = httptest.NewRecorder()
	guarded = setUntrustedAssetHeaders(rec, "/uploads/unknown")
	if _, err := guarded.(io.ReaderFrom).ReadFrom(strings.NewReader(testPNG)); err != nil {
		t.Fatal(err)
	}
	if got := rec.Header().Get("Content-Type"); got != "image/png" {
		t.Errorf("sniffed image: Content-Type = %q, want image/png", got)
	}
}
