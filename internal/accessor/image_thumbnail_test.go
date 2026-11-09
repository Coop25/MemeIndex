package accessor

import (
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// writeTestImage stores a width x height image under uploadDir and returns the
// meme row that would describe it.
func writeTestImage(t *testing.T, uploadDir, storedName, contentType string, width, height int) Meme {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: uint8(x ^ y), A: 255})
		}
	}
	file, err := os.Create(filepath.Join(uploadDir, storedName))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	switch contentType {
	case "image/png":
		err = png.Encode(file, img)
	case "image/jpeg":
		err = jpeg.Encode(file, img, nil)
	case "image/gif":
		err = gif.Encode(file, img, nil)
	default:
		t.Fatalf("unsupported test content type %q", contentType)
	}
	if err != nil {
		t.Fatal(err)
	}
	return Meme{StoredName: storedName, ContentType: contentType, FilePath: "/uploads/" + storedName}
}

func TestLargeImageGetsADownscaledThumbnail(t *testing.T) {
	ResetPreviewPathCache()
	uploadDir, previewDir := t.TempDir(), t.TempDir()
	meme := writeTestImage(t, uploadDir, "big.png", "image/png", 2000, 1000)

	result, err := ensurePreviewAssetWithResult(uploadDir, previewDir, &meme)
	if err != nil || result != previewAssetGenerated {
		t.Fatalf("result = %q, err = %v; want a generated thumbnail", result, err)
	}
	if meme.PreviewPath != "/thumbnails/big.jpg" {
		t.Fatalf("PreviewPath = %q, want the thumbnail", meme.PreviewPath)
	}
	file, err := os.Open(filepath.Join(previewDir, "big.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	config, err := jpeg.DecodeConfig(file)
	if err != nil {
		t.Fatalf("thumbnail is not a JPEG: %v", err)
	}
	if config.Width != 640 || config.Height != 320 {
		t.Fatalf("thumbnail is %dx%d, want 640x320 (aspect ratio kept)", config.Width, config.Height)
	}

	// A list request for the same meme must now point the grid at the thumbnail.
	listed := Meme{StoredName: "big.png", ContentType: "image/png", FilePath: "/uploads/big.png"}
	decoratePreviewPath(&listed, previewDir)
	if listed.PreviewPath != "/thumbnails/big.jpg" {
		t.Fatalf("listed PreviewPath = %q, want the thumbnail", listed.PreviewPath)
	}

	if again, err := ensurePreviewAssetWithResult(uploadDir, previewDir, &meme); err != nil || again != previewAssetAlreadyExists {
		t.Fatalf("second run = %q, err = %v; want already_exists", again, err)
	}
}

func TestSmallImageKeepsTheOriginalAsItsPreview(t *testing.T) {
	ResetPreviewPathCache()
	uploadDir, previewDir := t.TempDir(), t.TempDir()
	meme := writeTestImage(t, uploadDir, "small.png", "image/png", 120, 90)

	if _, err := ensurePreviewAssetWithResult(uploadDir, previewDir, &meme); err != nil {
		t.Fatal(err)
	}
	if meme.PreviewPath != "/uploads/small.png" {
		t.Fatalf("PreviewPath = %q, want the original (already small)", meme.PreviewPath)
	}
	if _, err := os.Stat(filepath.Join(previewDir, "small.jpg")); !os.IsNotExist(err) {
		t.Fatalf("a thumbnail was written for an already-small image (stat err %v)", err)
	}
}

// A static thumbnail would stop an animated GIF from animating in the grid.
func TestGIFKeepsTheOriginalAsItsPreview(t *testing.T) {
	ResetPreviewPathCache()
	uploadDir, previewDir := t.TempDir(), t.TempDir()
	meme := writeTestImage(t, uploadDir, "anim.gif", "image/gif", 1600, 1200)

	if _, err := ensurePreviewAssetWithResult(uploadDir, previewDir, &meme); err != nil {
		t.Fatal(err)
	}
	if meme.PreviewPath != "/uploads/anim.gif" {
		t.Fatalf("PreviewPath = %q, want the original GIF", meme.PreviewPath)
	}
}

func TestUndecodableImageFallsBackToTheOriginal(t *testing.T) {
	ResetPreviewPathCache()
	uploadDir, previewDir := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(uploadDir, "broken.png"), []byte("not really a png"), 0o600); err != nil {
		t.Fatal(err)
	}
	meme := Meme{StoredName: "broken.png", ContentType: "image/png", FilePath: "/uploads/broken.png"}

	if _, err := ensurePreviewAssetWithResult(uploadDir, previewDir, &meme); err != nil {
		t.Fatalf("an undecodable image must not fail the upload: %v", err)
	}
	if meme.PreviewPath != "/uploads/broken.png" {
		t.Fatalf("PreviewPath = %q, want the original", meme.PreviewPath)
	}
}
