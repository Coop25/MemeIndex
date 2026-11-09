package accessor

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteFileAtomicReplacesContents(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "index.json")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(path, []byte("new contents"), 0o644); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new contents" {
		t.Fatalf("contents = %q, want %q", got, "new contents")
	}
	assertNoTempFiles(t, dir)
}

// A failure after the payload is written but before it is committed (the
// moral equivalent of a crash or full disk) must leave the original file
// intact. With a plain os.WriteFile the original would already be truncated.
func TestMemeStoreSaveFailureKeepsPreviousFiles(t *testing.T) {
	dir := t.TempDir()
	store, err := NewMemeStore(dir)
	if err != nil {
		t.Fatalf("NewMemeStore: %v", err)
	}
	meme, err := store.Create(CreateInput{
		File:        strings.NewReader("png-bytes"),
		Filename:    "cat.png",
		ContentType: "image/png",
		Tags:        []string{"cat"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := store.SetFavorite("user-1", meme.ID, true); err != nil {
		t.Fatalf("SetFavorite: %v", err)
	}

	indexBefore := readFile(t, store.dataFile)
	favoritesBefore := readFile(t, store.favoritesFile)

	original := syncFile
	syncFile = func(*os.File) error { return errors.New("no space left on device") }
	t.Cleanup(func() { syncFile = original })

	if _, err := store.SetFavorite("user-1", meme.ID, false); err == nil {
		t.Fatal("SetFavorite succeeded despite failing write")
	}
	if err := store.RevokeMemeShare(meme.ID); err == nil {
		t.Fatal("RevokeMemeShare succeeded despite failing write")
	}

	if got := readFile(t, store.dataFile); got != indexBefore {
		t.Fatalf("index file changed after failed save:\n%s", got)
	}
	if got := readFile(t, store.favoritesFile); got != favoritesBefore {
		t.Fatalf("favorites file changed after failed save:\n%s", got)
	}
	assertNoTempFiles(t, dir)

	syncFile = original
	if _, err := NewMemeStore(dir); err != nil {
		t.Fatalf("reopening store after failed save: %v", err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Fatalf("leftover temp file %s", e.Name())
		}
	}
}
