package accessor

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

var ffmpegWarningOnce sync.Once

type previewAssetResult string

const (
	previewAssetUnsupported   previewAssetResult = "unsupported"
	previewAssetAlreadyExists previewAssetResult = "already_exists"
	previewAssetGenerated     previewAssetResult = "generated"
	previewAssetUnavailable   previewAssetResult = "unavailable"
)

func decoratePreviewPath(meme *Meme, previewDir string) {
	if meme == nil {
		return
	}

	switch {
	case strings.HasPrefix(meme.ContentType, "image/"):
		meme.PreviewPath = meme.FilePath
	case strings.HasPrefix(meme.ContentType, "video/"):
		thumbnailPath := thumbnailWebPath(meme.StoredName)
		if previewDir != "" {
			if _, err := os.Stat(filepath.Join(previewDir, thumbnailFileName(meme.StoredName))); err == nil {
				meme.PreviewPath = thumbnailPath
				return
			}
		}
		meme.PreviewPath = ""
	default:
		meme.PreviewPath = ""
	}
}

func ensurePreviewAsset(uploadDir, previewDir string, meme *Meme) error {
	_, err := ensurePreviewAssetWithResult(uploadDir, previewDir, meme)
	return err
}

func ensurePreviewAssetWithResult(uploadDir, previewDir string, meme *Meme) (previewAssetResult, error) {
	if meme == nil {
		return previewAssetUnsupported, nil
	}

	if strings.HasPrefix(meme.ContentType, "image/") {
		meme.PreviewPath = meme.FilePath
		return previewAssetUnsupported, nil
	}

	if !strings.HasPrefix(meme.ContentType, "video/") {
		meme.PreviewPath = ""
		return previewAssetUnsupported, nil
	}

	if previewDir == "" {
		meme.PreviewPath = ""
		return previewAssetUnavailable, nil
	}

	if err := os.MkdirAll(previewDir, 0o755); err != nil {
		return previewAssetUnavailable, err
	}

	inputPath := filepath.Join(uploadDir, meme.StoredName)
	outputPath := filepath.Join(previewDir, thumbnailFileName(meme.StoredName))
	if _, err := os.Stat(outputPath); errors.Is(err, os.ErrNotExist) {
		if err := generateVideoThumbnail(inputPath, outputPath); err != nil {
			meme.PreviewPath = ""
			return previewAssetUnavailable, err
		}
		meme.PreviewPath = thumbnailWebPath(meme.StoredName)
		return previewAssetGenerated, nil
	}

	meme.PreviewPath = thumbnailWebPath(meme.StoredName)
	return previewAssetAlreadyExists, nil
}

func thumbnailFileName(storedName string) string {
	base := strings.TrimSuffix(storedName, filepath.Ext(storedName))
	return base + ".jpg"
}

func thumbnailWebPath(storedName string) string {
	return "/thumbnails/" + thumbnailFileName(storedName)
}

func EnsureVideoTagFrames(uploadDir, previewDir, storedName string, frameCount int, frameWidth int) ([]string, error) {
	if strings.TrimSpace(uploadDir) == "" || strings.TrimSpace(previewDir) == "" || strings.TrimSpace(storedName) == "" {
		return nil, errors.New("video tag frames unavailable")
	}
	if frameCount <= 0 {
		frameCount = 3
	}
	if frameWidth <= 0 {
		frameWidth = 480
	}

	tagFrameDir := filepath.Join(previewDir, "tagframes")
	if err := os.MkdirAll(tagFrameDir, 0o755); err != nil {
		return nil, err
	}

	inputPath := filepath.Join(uploadDir, storedName)
	offsets := videoTagFrameOffsets(inputPath, frameCount)
	framePaths := make([]string, 0, len(offsets))
	var lastErr error
	for index, offset := range offsets {
		outputPath := filepath.Join(tagFrameDir, tagFrameFileName(storedName, index+1))
		if _, err := os.Stat(outputPath); errors.Is(err, os.ErrNotExist) {
			if err := generateVideoFrameAtOffset(inputPath, outputPath, offset, frameWidth); err != nil {
				lastErr = err
				continue
			}
		} else if err != nil {
			lastErr = err
			continue
		}

		framePaths = append(framePaths, outputPath)
	}

	if len(framePaths) == 0 && lastErr != nil {
		return nil, lastErr
	}
	if len(framePaths) == 0 {
		return nil, os.ErrNotExist
	}
	return framePaths, nil
}

func videoTagFrameOffsets(inputPath string, frameCount int) []string {
	if frameCount <= 0 {
		frameCount = 3
	}
	frameCount = min(frameCount, 5)
	cmd := exec.Command(
		"ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		inputPath,
	)
	output, err := cmd.Output()
	if err == nil {
		if duration, parseErr := strconv.ParseFloat(strings.TrimSpace(string(output)), 64); parseErr == nil && duration > 0 {
			return evenlySpacedVideoOffsets(duration, frameCount)
		}
	}

	fallback := []string{"0", "1", "3", "6", "10"}
	return fallback[:frameCount]
}

func evenlySpacedVideoOffsets(duration float64, frameCount int) []string {
	if frameCount <= 1 {
		return []string{"0"}
	}
	// Keep the last sample safely before EOF; seeking exactly to duration often
	// produces no frame on short or variable-frame-rate clips.
	end := duration * 0.88
	offsets := make([]string, 0, frameCount)
	for index := 0; index < frameCount; index++ {
		seconds := end * float64(index) / float64(frameCount-1)
		offsets = append(offsets, strconv.FormatFloat(seconds, 'f', 3, 64))
	}
	return offsets
}

func EnsureVideoTagAudio(uploadDir, previewDir, storedName string) (string, error) {
	if strings.TrimSpace(uploadDir) == "" || strings.TrimSpace(previewDir) == "" || strings.TrimSpace(storedName) == "" {
		return "", errors.New("video tag audio unavailable")
	}

	tagAudioDir := filepath.Join(previewDir, "tagaudio")
	if err := os.MkdirAll(tagAudioDir, 0o755); err != nil {
		return "", err
	}

	inputPath := filepath.Join(uploadDir, storedName)
	outputPath := filepath.Join(tagAudioDir, tagAudioFileName(storedName))
	if _, err := os.Stat(outputPath); err == nil {
		return outputPath, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	if err := extractVideoAudio(inputPath, outputPath); err != nil {
		return "", err
	}
	return outputPath, nil
}

func EnsureImageTagPreview(uploadDir, previewDir, storedName string, width int) (string, error) {
	if strings.TrimSpace(uploadDir) == "" || strings.TrimSpace(previewDir) == "" || strings.TrimSpace(storedName) == "" {
		return "", errors.New("image tag preview unavailable")
	}
	if width <= 0 {
		width = 480
	}

	tagImageDir := filepath.Join(previewDir, "tagimages")
	if err := os.MkdirAll(tagImageDir, 0o755); err != nil {
		return "", err
	}

	inputPath := filepath.Join(uploadDir, storedName)
	outputPath := filepath.Join(tagImageDir, thumbnailFileName(storedName))
	if _, err := os.Stat(outputPath); err == nil {
		return outputPath, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	if err := generateVideoFrameAtOffset(inputPath, outputPath, "00:00:00", width); err != nil {
		return "", err
	}
	return outputPath, nil
}

func generateVideoThumbnail(inputPath, outputPath string) error {
	return generateVideoFrameAtOffset(inputPath, outputPath, "00:00:01", 640)
}

func generateVideoFrameAtOffset(inputPath, outputPath string, offset string, width int) error {
	if width <= 0 {
		width = 640
	}
	cmd := exec.Command(
		"ffmpeg",
		"-y",
		"-ss", offset,
		"-i", inputPath,
		"-frames:v", "1",
		"-vf", fmt.Sprintf("scale=%d:-1", width),
		outputPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		ffmpegWarningOnce.Do(func() {
			log.Printf("video thumbnail generation is unavailable until ffmpeg is installed: %v", err)
		})
		if len(output) > 0 {
			log.Printf("ffmpeg output: %s", strings.TrimSpace(string(output)))
		}
		return err
	}
	return nil
}

func extractVideoAudio(inputPath, outputPath string) error {
	cmd := exec.Command(
		"ffmpeg",
		"-y",
		"-i", inputPath,
		"-vn",
		"-ac", "1",
		"-ar", "16000",
		"-t", "90",
		outputPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		ffmpegWarningOnce.Do(func() {
			log.Printf("video audio extraction is unavailable until ffmpeg is installed: %v", err)
		})
		if len(output) > 0 {
			log.Printf("ffmpeg output: %s", strings.TrimSpace(string(output)))
		}
		return err
	}
	return nil
}

func tagFrameFileName(storedName string, index int) string {
	base := strings.TrimSuffix(storedName, filepath.Ext(storedName))
	return fmt.Sprintf("%s-tag-v2-%d.jpg", base, index)
}

func tagAudioFileName(storedName string) string {
	base := strings.TrimSuffix(storedName, filepath.Ext(storedName))
	return base + "-tag.wav"
}
