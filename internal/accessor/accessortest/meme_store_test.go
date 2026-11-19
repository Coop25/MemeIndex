package accessortest

import (
	"errors"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"memeindex/internal/accessor"
)

// The in-memory store must name stored files the way PostgresStore does, so
// handler tests built on it see the same URL-safe /uploads/ paths.
func TestMemeStoreCreateSanitizesStoredExtension(t *testing.T) {
	store, err := NewMemeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, contentType, wantExt string
	}{
		{"image.png?size=large", "image/png", ".png"},
		{"clip.mp4#t=10", "video/mp4", ".mp4"},
		{"pic.PNG%3Fx", "image/png", ".png"},
		{"video.mp4 (1)", "video/mp4", ".mp4"},
		{`dir\name.gif\evil`, "image/gif", ".gif"},
		{"mystery.bin?x", "application/octet-stream", ""},
	}
	for i, tc := range cases {
		meme, err := store.Create(accessor.CreateInput{
			File:     strings.NewReader("hostile name bytes " + tc.name + string(rune('a'+i))),
			Header:   textproto.MIMEHeader{"Content-Type": []string{tc.contentType}},
			Filename: tc.name,
		})
		if err != nil {
			t.Fatalf("Create(%q): %v", tc.name, err)
		}
		if meme.OriginalName != tc.name {
			t.Errorf("OriginalName = %q, want %q unchanged", meme.OriginalName, tc.name)
		}
		if meme.StoredName != meme.ID+tc.wantExt {
			t.Errorf("%q: StoredName = %q, want %q", tc.name, meme.StoredName, meme.ID+tc.wantExt)
		}
		if meme.FilePath != "/uploads/"+meme.StoredName {
			t.Errorf("%q: FilePath = %q, want /uploads/%s", tc.name, meme.FilePath, meme.StoredName)
		}
		if _, err := os.Stat(filepath.Join(store.UploadDir(), meme.StoredName)); err != nil {
			t.Errorf("%q: stored file: %v", tc.name, err)
		}
	}
}

// RemoveSuggestedTag drops just the one tag from whatever list is stored now
// and reports a missing meme the way PostgresStore does.
func TestMemeStoreRemoveSuggestedTag(t *testing.T) {
	store, err := NewMemeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	meme, err := store.Create(accessor.CreateInput{
		File:     strings.NewReader("remove suggestion bytes"),
		Header:   textproto.MIMEHeader{"Content-Type": []string{"image/png"}},
		Filename: "pic.png",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceSuggestedTags(meme.ID, []string{"bird", "dog", "fish"}); err != nil {
		t.Fatal(err)
	}
	if err := store.RemoveSuggestedTag(meme.ID, " Dog "); err != nil {
		t.Fatalf("RemoveSuggestedTag: %v", err)
	}
	got, err := store.ListSuggestedTags(meme.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "bird,fish" {
		t.Fatalf("suggested tags = %v, want [bird fish]", got)
	}
	if err := store.RemoveSuggestedTag("missing", "dog"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing meme: err = %v, want os.ErrNotExist", err)
	}
}
