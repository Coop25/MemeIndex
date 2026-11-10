package accessor

import (
	"os"
	"path/filepath"
	"testing"
)

func writeScratchFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestRemoveTagScratchDeletesOnlyThatUploadsWorkingFiles(t *testing.T) {
	dir := t.TempDir()
	mine := []string{
		filepath.Join(dir, "tagframes", "abc-tag-v2-1.jpg"),
		filepath.Join(dir, "tagframes", "abc-tag-v2-3.jpg"),
		filepath.Join(dir, "tagframes", "abc-tag-2.jpg"), // pre-v2 name
		filepath.Join(dir, "tagimages", "abc.jpg"),
		filepath.Join(dir, "tagaudio", "abc-tag.wav"),
	}
	kept := []string{
		filepath.Join(dir, "abc.jpg"), // the real thumbnail
		filepath.Join(dir, "tagframes", "other-tag-v2-1.jpg"),
		filepath.Join(dir, "tagimages", "other.jpg"),
	}
	for _, path := range append(append([]string{}, mine...), kept...) {
		writeScratchFile(t, path)
	}

	RemoveTagScratch(dir, "abc.mp4")

	for _, path := range mine {
		if exists(path) {
			t.Errorf("%s was not removed", path)
		}
	}
	for _, path := range kept {
		if !exists(path) {
			t.Errorf("%s should have been kept", path)
		}
	}
}

func TestSweepTagScratchLeavesThumbnailsAlone(t *testing.T) {
	dir := t.TempDir()
	scratch := []string{
		filepath.Join(dir, "tagframes", "a-tag-1.jpg"),
		filepath.Join(dir, "tagframes", "a-tag-v2-1.jpg"),
		filepath.Join(dir, "tagimages", "b.jpg"),
		filepath.Join(dir, "tagaudio", "a-tag.wav"),
	}
	thumbnail := filepath.Join(dir, "a.jpg")
	for _, path := range append(append([]string{}, scratch...), thumbnail) {
		writeScratchFile(t, path)
	}

	if removed := SweepTagScratch(dir); removed != len(scratch) {
		t.Fatalf("removed = %d, want %d", removed, len(scratch))
	}
	for _, path := range scratch {
		if exists(path) {
			t.Errorf("%s was not removed", path)
		}
	}
	if !exists(thumbnail) {
		t.Fatal("the real thumbnail was removed")
	}
	if removed := SweepTagScratch(dir); removed != 0 {
		t.Fatalf("second sweep removed %d, want 0", removed)
	}
	if removed := SweepTagScratch(""); removed != 0 {
		t.Fatalf("empty dir sweep removed %d, want 0", removed)
	}
}
