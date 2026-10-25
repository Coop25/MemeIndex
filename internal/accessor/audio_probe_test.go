package accessor

import (
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRemuxMP3ToM4BKeepsChaptersAndAudio(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	metadata := filepath.Join(dir, "chapters.txt")
	if err := os.WriteFile(metadata, []byte(";FFMETADATA1\ntitle=Remux Test\nartist=Someone\n"+
		"[CHAPTER]\nTIMEBASE=1/1000\nSTART=0\nEND=20000\ntitle=Opening\n"+
		"[CHAPTER]\nTIMEBASE=1/1000\nSTART=20000\nEND=45000\ntitle=Middle\n"+
		"[CHAPTER]\nTIMEBASE=1/1000\nSTART=45000\nEND=60000\ntitle=End\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mp3 := filepath.Join(dir, "book.mp3")
	// -q:a makes libmp3lame produce VBR - the case that seeks badly.
	if out, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "anoisesrc=d=60:a=0.3", "-i", metadata,
		"-map", "0:a", "-map_metadata", "1", "-map_chapters", "1", "-c:a", "libmp3lame", "-q:a", "4", "-id3v2_version", "3", mp3).CombinedOutput(); err != nil {
		t.Fatalf("make test mp3: %v: %s", err, out)
	}

	m4b := filepath.Join(dir, "book.m4b")
	if err := RemuxMP3ToM4B(mp3, m4b); err != nil {
		t.Fatalf("RemuxMP3ToM4B: %v", err)
	}

	got, err := ProbeAudioMetadata(m4b)
	if err != nil {
		t.Fatalf("probe m4b: %v", err)
	}
	if got.Title != "Remux Test" || got.Artist != "Someone" {
		t.Errorf("tags = %q / %q, want Remux Test / Someone", got.Title, got.Artist)
	}
	var titles []string
	for _, c := range got.Chapters {
		titles = append(titles, c.Title)
	}
	if strings.Join(titles, ",") != "Opening,Middle,End" {
		t.Fatalf("chapters = %+v", got.Chapters)
	}
	if got.Chapters[1].StartSeconds < 19.9 || got.Chapters[1].StartSeconds > 20.1 {
		t.Errorf("chapter 2 starts at %.3f, want ~20", got.Chapters[1].StartSeconds)
	}

	codec, err := exec.Command("ffprobe", "-v", "error", "-select_streams", "a:0", "-show_entries", "stream=codec_name", "-of", "csv=p=0", m4b).Output()
	if err != nil || strings.TrimSpace(string(codec)) != "mp3" {
		t.Errorf("m4b audio codec = %q (%v), want mp3 (stream copy, no re-encode)", codec, err)
	}
}

// Several MP3s joined end to end - a common way audiobooks get assembled -
// keep only the first part's Xing header, so ffprobe's duration for the
// whole file is wrong: the first part's length, or (newer ffmpeg, noticing
// the header no longer matches the file size) an estimate from the opening
// bitrate. The remux has to be judged on what it actually carried across.
func TestRemuxMP3ToM4BAcceptsJoinedMP3s(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	first, second := filepath.Join(dir, "first.mp3"), filepath.Join(dir, "second.mp3")
	// Loud noise then a quiet tone: very different VBR bitrates, so a
	// bitrate-based estimate is far off.
	for _, part := range []struct {
		path   string
		source string
		extra  []string
	}{
		{first, "anoisesrc=d=20:a=0.5", nil},
		{second, "sine=d=40:f=220", []string{"-write_xing", "0", "-id3v2_version", "0"}},
	} {
		args := append([]string{"-v", "error", "-f", "lavfi", "-i", part.source, "-c:a", "libmp3lame", "-q:a", "4"}, part.extra...)
		if out, err := exec.Command("ffmpeg", append(args, part.path)...).CombinedOutput(); err != nil {
			t.Fatalf("make %s: %v: %s", part.path, err, out)
		}
	}
	firstBytes, _ := os.ReadFile(first)
	secondBytes, _ := os.ReadFile(second)
	joined := filepath.Join(dir, "joined.mp3")
	if err := os.WriteFile(joined, append(firstBytes, secondBytes...), 0o644); err != nil {
		t.Fatal(err)
	}
	if source, err := ProbeAudioMetadata(joined); err != nil || math.Abs(source.DurationSeconds-60) < 5 {
		t.Fatalf("expected ffprobe to misreport the joined file's 60s (got %.1fs, %v) - the test no longer exercises the bug", source.DurationSeconds, err)
	}

	m4b := filepath.Join(dir, "joined.m4b")
	if err := RemuxMP3ToM4B(joined, m4b); err != nil {
		t.Fatalf("RemuxMP3ToM4B: %v", err)
	}
	got, err := ProbeAudioMetadata(m4b)
	if err != nil {
		t.Fatal(err)
	}
	if got.DurationSeconds < 59 || got.DurationSeconds > 61 {
		t.Errorf("remuxed duration = %.2fs, want ~60s (both parts)", got.DurationSeconds)
	}
}

func TestRemuxMP3ToM4BRejectsNonAudio(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	bogus := filepath.Join(dir, "bogus.mp3")
	if err := os.WriteFile(bogus, []byte("not audio at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "bogus.m4b")
	if err := RemuxMP3ToM4B(bogus, out); err == nil {
		t.Fatal("RemuxMP3ToM4B succeeded on a non-audio file")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("output left behind after a failed remux: %v", err)
	}
}

func TestNormalizeAudioChapters(t *testing.T) {
	got := normalizeAudioChapters([]AudioChapter{
		{Title: "Two", StartSeconds: 600, EndSeconds: 600},  // zero-length end
		{Title: "One", StartSeconds: 0, EndSeconds: 700},    // overlaps the next chapter
		{Title: "Three", StartSeconds: 1200, EndSeconds: 0}, // missing end, last chapter
	}, 1800)
	want := []AudioChapter{
		{Title: "One", StartSeconds: 0, EndSeconds: 600},
		{Title: "Two", StartSeconds: 600, EndSeconds: 1200},
		{Title: "Three", StartSeconds: 1200, EndSeconds: 1800},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizeAudioChapters() = %+v, want %+v", got, want)
	}
}

func TestNormalizeAudioChaptersKeepsValidEnds(t *testing.T) {
	// A deliberate gap between chapters (an end before the next start) is
	// left alone - only missing, inverted, or overlapping ends are repaired.
	in := []AudioChapter{
		{Title: "One", StartSeconds: 0, EndSeconds: 590},
		{Title: "Two", StartSeconds: 600, EndSeconds: 1200},
	}
	got := normalizeAudioChapters(append([]AudioChapter(nil), in...), 1200)
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("normalizeAudioChapters() = %+v, want unchanged %+v", got, in)
	}
}
