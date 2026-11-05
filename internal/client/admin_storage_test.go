package client

import (
	"context"
	"net/textproto"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"memeindex/internal/accessor"
	"memeindex/internal/manager"
)

func writeSized(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMeasureStorage(t *testing.T) {
	root := t.TempDir()
	writeSized(t, filepath.Join(root, "uploads", "a.png"), 5)
	writeSized(t, filepath.Join(root, "uploads", "sub", "b.png"), 4)
	writeSized(t, filepath.Join(root, "thumbnails", "a.jpg"), 7)
	writeSized(t, filepath.Join(root, "audiobooks", "book.m4b"), 10)
	writeSized(t, filepath.Join(root, "audiobooks", "covers", "a.jpg"), 3)
	writeSized(t, filepath.Join(root, "audiobooks", "tmp", "u1.part"), 6)
	writeSized(t, filepath.Join(root, "downloads", "d.bin"), 11)
	writeSized(t, filepath.Join(root, "backups", "p.tar"), 12)
	writeSized(t, filepath.Join(root, "notes.txt"), 2)
	if err := os.Symlink(filepath.Join(root, "audiobooks"), filepath.Join(root, "uploads", "link")); err != nil {
		t.Logf("symlink unavailable, skipping symlink case: %v", err)
	}

	got, err := measureStorage(root)
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		name  string
		bytes int64
		files int
	}
	want := []row{
		{"uploads", 9, 2}, {"thumbnails", 7, 1}, {"audiobooks", 10, 1},
		{"audiobooks/covers", 3, 1}, {"audiobooks/tmp", 6, 1},
		{"downloads", 11, 1}, {"backups", 12, 1}, {"other", 2, 1},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Name != w.name || got[i].Bytes != w.bytes || got[i].Files != w.files {
			t.Errorf("entry %d = %+v, want %+v", i, got[i], w)
		}
	}
	if got[0].Path != filepath.Join(root, "uploads") {
		t.Errorf("uploads path = %q", got[0].Path)
	}

	empty, err := measureStorage(filepath.Join(root, "missing"))
	if err != nil || len(empty) != 8 || empty[0].Bytes != 0 {
		t.Errorf("missing dir = %+v, %v; want 8 zero entries", empty, err)
	}
}

func TestFindOrphans(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	old := now.Add(-48 * time.Hour)
	mk := func(rel string, mtime time.Time) string {
		p := filepath.Join(root, filepath.FromSlash(rel))
		writeSized(t, p, 3)
		if err := os.Chtimes(p, mtime, mtime); err != nil {
			t.Fatal(err)
		}
		return p
	}
	refA := mk("uploads/a.png", old)
	mk("uploads/b.png", old)
	mk("uploads/c.png", now)
	mk("uploads/.hidden", old)
	mk("uploads/sub/deep.png", old)
	mk("thumbnails/t-old.jpg", old)
	mk("audiobooks/x.mp3.remux-failed", old)
	mk("audiobooks/ref.mp3", old)
	mk("audiobooks/ref.m4b", old) // in-flight remux output of a referenced mp3
	mk("audiobooks/stray.m4b", old)
	mk("audiobooks/covers/orphan.jpg", old)
	mk("audiobooks/tmp/u1", old)
	mk("audiobooks/tmp/u2.part", old)
	mk("backups/p1.part", old)
	mk("downloads/x", old)
	if err := os.Symlink(refA, filepath.Join(root, "uploads", "link.png")); err != nil {
		t.Logf("symlink unavailable, skipping symlink case: %v", err)
	}

	refs := storageReferences{
		Paths: map[string]struct{}{
			refA: {},
			filepath.Join(root, "audiobooks", "ref.mp3"):        {},
			filepath.Join(root, "audiobooks", "tmp", "u2.part"): {},
		},
		ScanAudiobooks: true,
	}
	got, err := findOrphans(root, refs, now)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, o := range got {
		rel, _ := filepath.Rel(root, o.Path)
		paths = append(paths, filepath.ToSlash(rel))
		if o.Bytes != 3 || o.ModifiedAt.IsZero() || o.Dir == "" {
			t.Errorf("bad orphan fields: %+v", o)
		}
	}
	sort.Strings(paths)
	want := []string{"audiobooks/covers/orphan.jpg", "audiobooks/stray.m4b", "audiobooks/tmp/u1", "thumbnails/t-old.jpg", "uploads/b.png"}
	if !reflect.DeepEqual(paths, want) {
		t.Errorf("orphans = %v, want %v", paths, want)
	}

	// Without a DB, nothing under audiobooks may be reported.
	refs.ScanAudiobooks = false
	got, err = findOrphans(root, refs, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range got {
		if strings.Contains(filepath.ToSlash(o.Path), "/audiobooks/") {
			t.Errorf("audiobook orphan reported without DB: %s", o.Path)
		}
	}
}

type storagePathsStore struct {
	audiobookStore
	books []audiobookStoragePath
	temps []string
}

func (s storagePathsStore) ListStoragePaths(context.Context) ([]audiobookStoragePath, error) {
	return s.books, nil
}
func (s storagePathsStore) ListUploadTempPaths(context.Context) ([]string, error) {
	return s.temps, nil
}

func TestCollectStorageReferences(t *testing.T) {
	dataDir := t.TempDir()
	store, err := accessor.NewMemeStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	meme, err := store.Create(accessor.CreateInput{
		File:        strings.NewReader("png bytes"),
		Header:      textproto.MIMEHeader{"Content-Type": []string{"image/png"}},
		Filename:    "m.png",
		ContentType: "image/png",
	})
	if err != nil {
		t.Fatal(err)
	}
	ghost, err := store.Create(accessor.CreateInput{
		File:        strings.NewReader("gone"),
		Header:      textproto.MIMEHeader{"Content-Type": []string{"image/png"}},
		Filename:    "ghost.png",
		ContentType: "image/png",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(store.UploadDir(), ghost.StoredName)); err != nil {
		t.Fatal(err)
	}

	bookFile := filepath.Join(dataDir, "audiobooks", "b.m4b")
	writeSized(t, bookFile, 1)
	coverFile := filepath.Join(dataDir, "audiobooks", "covers", "b.jpg")
	tempFile := filepath.Join(dataDir, "audiobooks", "tmp", "u.part")
	s := &Server{
		config:   Config{DataDir: dataDir},
		managers: manager.NewMemeManager(store),
		audiobooks: storagePathsStore{
			books: []audiobookStoragePath{
				{ID: "b1", Title: "B", StoragePath: bookFile, CoverPath: coverFile},
				{ID: "b2", Title: "NoCover", StoragePath: filepath.Join(dataDir, "audiobooks", "gone.m4b")},
			},
			temps: []string{tempFile},
		},
	}
	refs, missing, err := s.collectStorageReferences(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{
		filepath.Join(store.UploadDir(), meme.StoredName),
		filepath.Join(store.UploadDir(), ghost.StoredName),
		bookFile, coverFile, tempFile,
	} {
		if _, ok := refs.Paths[p]; !ok {
			t.Errorf("missing reference %s", p)
		}
	}
	if !refs.ScanAudiobooks {
		t.Error("ScanAudiobooks should be true with a DB")
	}
	kinds := map[string]string{}
	for _, m := range missing {
		kinds[m.Kind+":"+m.ID] = m.Path
	}
	if _, ok := kinds["meme:"+ghost.ID]; !ok {
		t.Errorf("ghost meme not reported missing: %+v", missing)
	}
	if _, ok := kinds["audiobook:b2"]; !ok {
		t.Errorf("b2 not reported missing: %+v", missing)
	}
	if _, ok := kinds["cover:b1"]; !ok {
		t.Errorf("b1 cover not reported missing: %+v", missing)
	}
	if len(missing) != 3 {
		t.Errorf("missing = %+v, want 3", missing)
	}

	s.audiobooks = nil
	refs, _, err = s.collectStorageReferences(context.Background())
	if err != nil || refs.ScanAudiobooks {
		t.Errorf("no DB: ScanAudiobooks=%v err=%v, want false", refs.ScanAudiobooks, err)
	}
}

func TestListStoragePathsAndUploadTempPaths(t *testing.T) {
	store, _ := newIsolatedAudiobookStore(t)
	ctx := context.Background()
	id := insertActivityBook(t, store, "Book", "/c/a.jpg", "[]", "")
	books, err := store.ListStoragePaths(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 1 || books[0].ID != id || books[0].Title != "Book" || books[0].StoragePath != "/tmp/book.m4b" || books[0].CoverPath != "/c/a.jpg" {
		t.Errorf("books = %+v", books)
	}
	upload, err := store.CreateUpload(ctx, audiobookUploadSeed{UserID: "u1", Title: "T", Filename: "t.mp3", MimeType: "audio/mpeg", TotalBytes: 10})
	if err != nil {
		t.Fatal(err)
	}
	temps, err := store.ListUploadTempPaths(ctx)
	if err != nil || len(temps) != 1 || temps[0] != upload.TempPath {
		t.Errorf("temps = %v, %v; want [%s]", temps, err, upload.TempPath)
	}
}
