package accessor

import (
	"context"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoredFileExtension(t *testing.T) {
	for _, tc := range []struct {
		filename, contentType, want string
	}{
		{"cat.png", "image/png", ".png"},
		{"CAT.PNG", "image/png", ".png"},
		{"clip.MP4", "", ".mp4"},
		{"archive.tar.gz", "application/gzip", ".gz"},
		{"photo.jpeg", "image/jpeg", ".jpeg"},
		{"meme.webm", "video/webm", ".webm"},
		// Kept when it is plain, whatever the type says: the file server
		// types it from the extension.
		{"notes.txt", "image/png", ".txt"},

		// Hostile or broken extensions fall back to the detected type.
		{"image.png?size=large", "image/png", ".png"},
		{"clip.mp4#t=10", "video/mp4", ".mp4"},
		{"pic.png%3Fx", "image/png", ".png"},
		{"pic.p ng", "image/jpeg", ".jpg"},
		{"video.mp4 (1)", "video/mp4", ".mp4"},
		{`x.png\..\..\evil`, "image/png", ".png"},
		{"x.ünï", "image/gif", ".gif"},
		{"x.abcdefghijk", "image/webp", ".webp"},
		{"x.", "image/png", ".png"},
		{"x.png\r\n", "image/png", ".png"},
		{"x.mov;type=evil", "video/quicktime; charset=binary", ".mov"},

		// No usable type either: no extension, rather than a made-up one.
		{"image.png?size=large", "application/octet-stream", ""},
		{"no-extension", "", ""},
		{"page.ht ml", "text/html", ""},
		{"script.j s", "application/javascript", ""},
		{"x.%00", "not a type", ""},
		{"", "", ""},
	} {
		if got := StoredFileExtension(tc.filename, tc.contentType); got != tc.want {
			t.Errorf("StoredFileExtension(%q, %q) = %q, want %q", tc.filename, tc.contentType, got, tc.want)
		}
	}
}

// assertStoredNameResolves checks a created meme's stored name is a single,
// URL-safe path segment whose /uploads/ link points back at the file on disk,
// and that the name the user gave is kept for display.
func assertStoredNameResolves(t *testing.T, meme Meme, uploadDir, originalName, wantExt string) {
	t.Helper()
	if meme.OriginalName != originalName {
		t.Errorf("OriginalName = %q, want %q unchanged", meme.OriginalName, originalName)
	}
	if meme.StoredName != meme.ID+wantExt {
		t.Errorf("StoredName = %q, want %q", meme.StoredName, meme.ID+wantExt)
	}
	parsed, err := url.Parse(meme.FilePath)
	if err != nil || parsed.Path != meme.FilePath || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.EscapedPath() != meme.FilePath {
		t.Errorf("FilePath %q does not survive as a URL path (err %v, parsed %+v)", meme.FilePath, err, parsed)
	}
	if meme.FilePath != "/uploads/"+meme.StoredName {
		t.Errorf("FilePath = %q, want /uploads/%s", meme.FilePath, meme.StoredName)
	}
	if _, err := os.Stat(filepath.Join(uploadDir, strings.TrimPrefix(parsed.Path, "/uploads/"))); err != nil {
		t.Errorf("file behind %q: %v", meme.FilePath, err)
	}
}

var hostileUploadNames = []struct {
	name, contentType, wantExt string
}{
	{"image.png?size=large", "image/png", ".png"},
	{"clip.mp4#t=10", "video/mp4", ".mp4"},
	{"pic.PNG%3Fx", "image/png", ".png"},
	{"video.mp4 (1)", "video/mp4", ".mp4"},
	{`dir\name.gif\evil`, "image/gif", ".gif"},
	{"mystery.bin?x", "application/octet-stream", ""},
}

func TestPostgresStoreCreateSanitizesStoredExtension(t *testing.T) {
	store := newRegressionStore(t)
	token := "ext" + randToken(t)
	for _, tc := range hostileUploadNames {
		name := token + "-" + tc.name
		meme, err := store.Create(context.Background(), CreateInput{
			File:     strings.NewReader("hostile name bytes " + name),
			Header:   textproto.MIMEHeader{"Content-Type": []string{tc.contentType}},
			Filename: name,
		})
		if err != nil {
			t.Fatalf("Create(%q): %v", name, err)
		}
		assertStoredNameResolves(t, meme, store.UploadDir(), name, tc.wantExt)

		// What was saved is what comes back.
		saved, err := store.GetAnyByID(context.Background(), meme.ID)
		if err != nil {
			t.Fatalf("GetAnyByID(%s): %v", meme.ID, err)
		}
		if saved.StoredName != meme.StoredName || saved.FilePath != meme.FilePath || saved.OriginalName != name {
			t.Errorf("saved row = %q / %q / %q, want %q / %q / %q", saved.StoredName, saved.FilePath, saved.OriginalName, meme.StoredName, meme.FilePath, name)
		}
	}
}
