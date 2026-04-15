package accessor

import (
	"errors"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

var ffmpegWarningOnce sync.Once

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
	if meme == nil {
		return nil
	}

	if strings.HasPrefix(meme.ContentType, "image/") {
		meme.PreviewPath = meme.FilePath
		return nil
	}

	if !strings.HasPrefix(meme.ContentType, "video/") {
		meme.PreviewPath = ""
		return nil
	}

	if previewDir == "" {
		meme.PreviewPath = ""
		return nil
	}

	if err := os.MkdirAll(previewDir, 0o755); err != nil {
		return err
	}

	inputPath := filepath.Join(uploadDir, meme.StoredName)
	outputPath := filepath.Join(previewDir, thumbnailFileName(meme.StoredName))
	if _, err := os.Stat(outputPath); errors.Is(err, os.ErrNotExist) {
		if err := generateVideoThumbnail(inputPath, outputPath); err != nil {
			meme.PreviewPath = ""
			return err
		}
	}

	meme.PreviewPath = thumbnailWebPath(meme.StoredName)
	return nil
}

func thumbnailFileName(storedName string) string {
	base := strings.TrimSuffix(storedName, filepath.Ext(storedName))
	return base + ".jpg"
}

func thumbnailWebPath(storedName string) string {
	return "/thumbnails/" + thumbnailFileName(storedName)
}

func generateVideoThumbnail(inputPath, outputPath string) error {
	cmd := exec.Command(
		"ffmpeg",
		"-y",
		"-ss", "00:00:01",
		"-i", inputPath,
		"-frames:v", "1",
		"-vf", "scale=640:-1",
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
