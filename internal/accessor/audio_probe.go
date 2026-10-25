package accessor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
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

// runProbeCommand keeps stderr apart from stdout: ffprobe can print a
// demuxer complaint (a damaged frame, an odd tag) and still exit 0, and
// interleaving that text into the JSON would make it unparsable - losing
// every chapter over one harmless warning.
func runProbeCommand(cmd *exec.Cmd) ([]byte, error) {
	var output, stderr boundedProbeOutput
	cmd.Stdout, cmd.Stderr = &output, &stderr
	err := cmd.Run()
	if err != nil && len(stderr.data) > 0 {
		err = fmt.Errorf("%w: %s", err, strings.TrimSpace(string(stderr.data)))
	}
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
	meta.Chapters = normalizeAudioChapters(meta.Chapters, meta.DurationSeconds)

	return meta, nil
}

// normalizeAudioChapters puts chapter markers in playback order and repairs
// the end times the player relies on for its chapter-relative seek bar.
// Taggers - ID3 CHAP writers especially - commonly leave the end time at
// zero, equal to the start, or overlapping the next chapter; each chapter is
// taken to run until the next one starts, and the last until the end of the
// file.
func normalizeAudioChapters(chapters []AudioChapter, durationSeconds float64) []AudioChapter {
	if len(chapters) == 0 {
		return chapters
	}
	sort.SliceStable(chapters, func(i, j int) bool { return chapters[i].StartSeconds < chapters[j].StartSeconds })
	for i := range chapters {
		if chapters[i].StartSeconds < 0 {
			chapters[i].StartSeconds = 0
		}
		limit := durationSeconds
		if i+1 < len(chapters) {
			limit = chapters[i+1].StartSeconds
		}
		end := chapters[i].EndSeconds
		if end <= chapters[i].StartSeconds || (limit > chapters[i].StartSeconds && end > limit) {
			end = limit
		}
		chapters[i].EndSeconds = end
	}
	return chapters
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

// remuxTimeout bounds one remux. It's a stream copy, so it's I/O-bound - a
// 700 MB, 15-hour book takes well under a minute on ordinary disks - but the
// source is read twice (copy, then packet count) and +faststart rewrites the
// output once more to put the index first, so the budget grows with the
// file: MEMEINDEX_MAX_AUDIOBOOK_BYTES allows books of up to 20 GB, which a
// slow disk or NAS can't get through in a fixed few minutes.
func remuxTimeout(sizeBytes int64) time.Duration {
	const perGB = 2 * time.Minute
	return 15*time.Minute + time.Duration(sizeBytes/(1<<30))*perGB
}

// RemuxMP3ToM4B copies an MP3's audio frames, unchanged, into an MP4 (.m4b)
// container at outputPath, keeping its tags and chapter markers.
//
// The point is seeking. A browser can't compute where a timestamp falls in a
// VBR MP3, so it estimates from the 100-entry Xing table of contents - off by
// tens of seconds or more in a long audiobook, which is what made chapter
// jumps land in the wrong place. An MP4 carries a per-frame index, so every
// browser seeks exactly. No re-encode happens, so there's no quality loss.
//
// Before this returns nil the output is checked to hold exactly as many
// audio packets as the source; on any failure outputPath is removed and the
// caller should keep serving the original file.
func RemuxMP3ToM4B(inputPath, outputPath string) error {
	info, err := os.Stat(inputPath)
	if err != nil {
		return fmt.Errorf("stat source: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), remuxTimeout(info.Size()))
	defer cancel()

	cmd := localMediaCommand(ctx,
		"ffmpeg",
		"-y",
		"-i", inputPath,
		// Audio only: an embedded cover is an attached-picture video stream,
		// which the cover-art pipeline already extracted separately.
		"-map", "0:a:0",
		"-c:a", "copy",
		"-map_metadata", "0",
		"-map_chapters", "0",
		// Index (moov) at the front, so playback and seeking can start
		// without first fetching the end of a multi-hundred-MB file.
		"-movflags", "+faststart",
		"-f", "mp4",
		outputPath,
	)
	if output, err := runMediaCommand(cmd); err != nil {
		_ = os.Remove(outputPath)
		return fmt.Errorf("ffmpeg remux: %w: %s", err, strings.TrimSpace(string(output)))
	}

	if err := verifyRemuxedPackets(ctx, inputPath, outputPath); err != nil {
		_ = os.Remove(outputPath)
		return err
	}
	return nil
}

// verifyRemuxedPackets compares audio packet counts rather than durations:
// ffprobe's duration for an MP3 is only exact when it carries an accurate
// Xing/Info header - for one without, or several MP3s joined end to end
// (common for audiobooks), it's an estimate that can be far off, while the
// remux reports the true length. A stream copy must carry every packet
// across. The source count needs a full read of the MP3 (it has no index -
// the whole problem), which right after the remux mostly comes from the page
// cache; the MP4's count is read straight from its index.
func verifyRemuxedPackets(ctx context.Context, sourcePath, remuxedPath string) error {
	sourcePackets, err := probeAudioPacketCount(ctx, sourcePath, true)
	if err != nil {
		return fmt.Errorf("count source packets: %w", err)
	}
	remuxedPackets, err := probeAudioPacketCount(ctx, remuxedPath, false)
	if err != nil {
		return fmt.Errorf("count remuxed packets: %w", err)
	}
	if sourcePackets == 0 || sourcePackets != remuxedPackets {
		return fmt.Errorf("remuxed file has %d audio packets, source has %d", remuxedPackets, sourcePackets)
	}
	return nil
}

// probeAudioPacketCount returns the first audio stream's packet count:
// read from the container's index, or, with countPackets, by demuxing the
// whole file for containers that have none.
func probeAudioPacketCount(ctx context.Context, path string, countPackets bool) (int64, error) {
	args := []string{"-select_streams", "a:0", "-show_entries", "stream=nb_frames", "-of", "csv=p=0", path}
	if countPackets {
		args = []string{"-select_streams", "a:0", "-count_packets", "-show_entries", "stream=nb_read_packets", "-of", "csv=p=0", path}
	}
	output, err := runProbeCommand(localMediaCommand(ctx, "ffprobe", args...))
	if err != nil {
		return 0, err
	}
	value, _, _ := strings.Cut(strings.TrimSpace(string(output)), ",")
	return strconv.ParseInt(strings.TrimSpace(value), 10, 64)
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
