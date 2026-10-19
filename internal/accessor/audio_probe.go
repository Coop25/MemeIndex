package accessor

import (
	"context"
	"encoding/json"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// AudioMetadata is what ffprobe can tell us about an uploaded audio file
// without decoding it: its duration, whatever free-text tags (title, artist,
// album, ...) are embedded in the container - ID3 for MP3, atoms for M4A/M4B,
// Vorbis comments for OGG/FLAC, and so on - and any chapter markers (common
// in M4B audiobooks, and in MP3s tagged with ID3v2 CHAP/CTOC frames).
type AudioMetadata struct {
	DurationSeconds float64
	Title           string
	Artist          string
	Narrator        string
	Album           string
	Chapters        []AudioChapter
}

// AudioChapter is one chapter marker read from the file's own chapter atom.
type AudioChapter struct {
	Title        string
	StartSeconds float64
	EndSeconds   float64
}

type ffprobeFormat struct {
	Duration string            `json:"duration"`
	Tags     map[string]string `json:"tags"`
}

type ffprobeChapter struct {
	StartTime string            `json:"start_time"`
	EndTime   string            `json:"end_time"`
	Tags      map[string]string `json:"tags"`
}

type ffprobeOutput struct {
	Format   ffprobeFormat    `json:"format"`
	Chapters []ffprobeChapter `json:"chapters"`
}

// probeOutputLimit is generous relative to runMediaCommand's 16 KiB cap used
// elsewhere in this file: a long audiobook can carry hundreds of chapter
// markers, and truncating that JSON mid-object would make the whole response
// unparsable rather than just losing the tail end of it.
const probeOutputLimit = 1 << 20 // 1 MiB

type boundedProbeOutput struct{ data []byte }

func (b *boundedProbeOutput) Write(p []byte) (int, error) {
	n := len(p)
	if remaining := probeOutputLimit - len(b.data); remaining > 0 {
		b.data = append(b.data, p[:min(remaining, len(p))]...)
	}
	return n, nil
}

func runProbeCommand(cmd *exec.Cmd) ([]byte, error) {
	var output boundedProbeOutput
	cmd.Stdout, cmd.Stderr = &output, &output
	err := cmd.Run()
	return output.data, err
}

// ProbeAudioMetadata reads the duration, embedded tags, and chapter markers
// of a local audio file via ffprobe, using the same hardened invocation
// (local files only, a fixed demuxer whitelist) as the video preview
// pipeline. An error here means "no metadata available" - callers should
// fall back to filenames/client-supplied values rather than fail the upload
// over it.
func ProbeAudioMetadata(path string) (AudioMetadata, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// Ask for every format/chapter tag rather than a fixed key list: audiobook
	// tagging is not standardized the way music ID3 tags are, so the author
	// might show up as "artist", or - common in Audible-derived M4Bs - as a
	// "performer" freeform atom with "artist" absent entirely.
	cmd := localMediaCommand(ctx,
		"ffprobe",
		"-show_entries", "format=duration:format_tags:chapter=start_time,end_time:chapter_tags",
		"-of", "json=compact=1",
		path,
	)
	output, err := runProbeCommand(cmd)
	if err != nil {
		return AudioMetadata{}, err
	}

	var parsed ffprobeOutput
	if err := json.Unmarshal(output, &parsed); err != nil {
		return AudioMetadata{}, err
	}

	meta := AudioMetadata{
		Title:    strings.TrimSpace(audioTagValue(parsed.Format.Tags, "title")),
		Artist:   strings.TrimSpace(firstAudioTagValue(parsed.Format.Tags, "artist", "performer", "author", "album_artist")),
		Narrator: strings.TrimSpace(audioTagValue(parsed.Format.Tags, "narrator")),
		Album:    strings.TrimSpace(audioTagValue(parsed.Format.Tags, "album")),
	}
	if duration, parseErr := strconv.ParseFloat(strings.TrimSpace(parsed.Format.Duration), 64); parseErr == nil && duration > 0 {
		meta.DurationSeconds = duration
	}

	for i, chapter := range parsed.Chapters {
		start, startErr := strconv.ParseFloat(strings.TrimSpace(chapter.StartTime), 64)
		end, endErr := strconv.ParseFloat(strings.TrimSpace(chapter.EndTime), 64)
		if startErr != nil || endErr != nil {
			continue
		}
		title := strings.TrimSpace(audioTagValue(chapter.Tags, "title"))
		if title == "" {
			title = "Chapter " + strconv.Itoa(i+1)
		}
		meta.Chapters = append(meta.Chapters, AudioChapter{
			Title:        title,
			StartSeconds: start,
			EndSeconds:   end,
		})
	}

	return meta, nil
}

// audioTagValue looks up a format/chapter tag case-insensitively: ffprobe
// lowercases tag keys for some containers (MP4) but preserves ID3's mixed
// case for others.
func audioTagValue(tags map[string]string, key string) string {
	if value, ok := tags[key]; ok {
		return value
	}
	for k, v := range tags {
		if strings.EqualFold(k, key) {
			return v
		}
	}
	return ""
}

// firstAudioTagValue tries each key in order and returns the first non-blank
// match.
func firstAudioTagValue(tags map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(audioTagValue(tags, key)); value != "" {
			return value
		}
	}
	return ""
}

// ExtractEmbeddedCoverArt pulls the first attached picture out of an audio
// file (an ID3 APIC frame in MP3, the "covr" atom in M4A/M4B, ...) and
// re-encodes it to a JPEG at outputPath. ffmpeg exposes an embedded picture
// as a one-frame video stream, so this reuses the same "grab a frame"
// technique as video thumbnails. Most audio files have no embedded art at
// all, which ffmpeg reports as a normal non-zero exit; that is not logged as
// a warning the way a missing ffmpeg binary is.
func ExtractEmbeddedCoverArt(inputPath, outputPath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := localMediaCommand(ctx,
		"ffmpeg",
		"-y",
		"-i", inputPath,
		"-an",
		"-frames:v", "1",
		outputPath,
	)
	_, err := runMediaCommand(cmd)
	return err
}
