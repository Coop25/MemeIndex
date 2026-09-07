package accessor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestLocalMediaCommandsRestrictProtocolsAndDemuxers(t *testing.T) {
	for _, binary := range []string{"ffmpeg", "ffprobe"} {
		cmd := localMediaCommand(context.Background(), binary, "-i", "upload.mp4")
		args := strings.Join(cmd.Args, " ")
		if !strings.Contains(args, "-protocol_whitelist file") || !strings.Contains(args, "-format_whitelist "+localMediaFormats) {
			t.Fatal(args)
		}
		for _, format := range strings.Split(localMediaFormats, ",") {
			if format == "hls" || format == "concat" || format == "dash" || format == "image2" {
				t.Fatalf("external-reference demuxer allowed: %s", format)
			}
		}
	}
}

func TestLocalMediaProcessingIntegration(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not installed")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe is not installed")
	}
	dir := t.TempDir()
	input := filepath.Join(dir, "fixture.mp4")
	cmd := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "color=c=black:s=32x32:r=10", "-f", "lavfi", "-i", "sine=frequency=440", "-t", "1", "-c:v", "mpeg4", "-c:a", "aac", input)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v: %s", err, output)
	}
	if err := generateVideoFrameAtOffset(input, filepath.Join(dir, "frame.jpg"), "0", 32); err != nil {
		t.Fatal(err)
	}
	if err := extractVideoAudio(input, filepath.Join(dir, "audio.wav")); err != nil {
		t.Fatal(err)
	}
	if offsets := videoTagFrameOffsets(input, 3); len(offsets) != 3 || offsets[1] == "1" {
		t.Fatalf("probe fell back: %v", offsets)
	}
	// A playlist can refer to a media file outside its own upload directory.
	uploadDir := filepath.Join(dir, "uploads")
	if err := os.Mkdir(uploadDir, 0700); err != nil {
		t.Fatal(err)
	}
	playlist := filepath.Join(uploadDir, "untrusted.m3u8")
	payload := "#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXT-X-MEDIA-SEQUENCE:0\n#EXTINF:1,\n" + input + "\n#EXT-X-ENDLIST\n"
	if err := os.WriteFile(playlist, []byte(payload), 0600); err != nil {
		t.Fatal(err)
	}
	baseline := exec.Command("ffprobe", "-v", "error", "-show_entries", "format=duration", playlist)
	if output, err := baseline.CombinedOutput(); err != nil {
		t.Fatalf("playlist reproduction: %v: %s", err, output)
	}
	if _, err := runMediaCommand(localMediaCommand(context.Background(), "ffprobe", "-show_entries", "format=duration", playlist)); err == nil {
		t.Fatal("untrusted playlist was accepted")
	}
}

func TestMediaDiagnosticsStayBounded(t *testing.T) {
	var output boundedMediaOutput
	payload := []byte(strings.Repeat("x", 20000))
	for i := 0; i < 5; i++ {
		if n, err := output.Write(payload); n != len(payload) || err != nil {
			t.Fatal(n, err)
		}
	}
	if len(output.data) != 16*1024 {
		t.Fatal(len(output.data))
	}
}

func TestEvenlySpacedVideoOffsetsCoversClip(t *testing.T) {
	offsets := evenlySpacedVideoOffsets(10, 3)
	if len(offsets) != 3 || offsets[0] != "0.000" {
		t.Fatalf("unexpected offsets %v", offsets)
	}
	middle, err := strconv.ParseFloat(offsets[1], 64)
	if err != nil || middle < 4 || middle > 5 {
		t.Fatalf("middle frame did not sample the middle of the clip: %v", offsets)
	}
	last, err := strconv.ParseFloat(offsets[2], 64)
	if err != nil || last < 8 || last >= 10 {
		t.Fatalf("last frame did not sample near the end of the clip: %v", offsets)
	}
}

func TestEvenlySpacedVideoOffsetsSingleFrameStartsAtBeginning(t *testing.T) {
	offsets := evenlySpacedVideoOffsets(15, 1)
	if len(offsets) != 1 || offsets[0] != "0" {
		t.Fatalf("unexpected offsets %v", offsets)
	}
}
