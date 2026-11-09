package accessor

import (
	"bufio"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	_ "image/png" // register the PNG decoder for image.Decode
	"io"
	"os"
	"path/filepath"
)

// Grid cards are a few hundred CSS pixels wide, so a 640px-wide JPEG covers a
// 2x display. The height cap keeps a very tall screenshot from producing a
// thumbnail as heavy as the original.
const (
	imageThumbnailMaxWidth  = 640
	imageThumbnailMaxHeight = 1280
	// An image already within the thumbnail bounds and under this size is
	// served as its own preview; re-encoding it would save nothing.
	imageThumbnailSkipBytes = 200 << 10
	// Decoding holds the whole bitmap in memory (twice, briefly). Past this
	// many pixels the original stays the preview instead of risking a
	// several-hundred-megabyte spike on the upload path.
	imageThumbnailMaxPixels = 16_000_000
	imageThumbnailQuality   = 82
)

// imageThumbnailBackground sits behind transparent pixels, which JPEG cannot
// store. It matches the dark card the grid draws transparent images on.
var imageThumbnailBackground = color.RGBA{R: 0x11, G: 0x14, B: 0x18, A: 0xff}

// errImageThumbnailSkipped reports that an image should keep its original file
// as the preview: it is already small, animated, too large to decode safely,
// or in a format the standard library cannot read.
var errImageThumbnailSkipped = errors.New("image thumbnail skipped")

// generateImageThumbnail writes a downscaled JPEG of the image at inputPath to
// outputPath. It returns errImageThumbnailSkipped when the original should be
// used as the preview instead.
func generateImageThumbnail(inputPath, outputPath string) error {
	file, err := os.Open(inputPath)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}

	config, format, err := image.DecodeConfig(bufio.NewReader(file))
	if err != nil {
		// WebP, AVIF, SVG and anything corrupt: no decoder, keep the original.
		return errImageThumbnailSkipped
	}
	// Only JPEG and PNG are thumbnailed. GIF is left alone on purpose: a
	// static thumbnail would stop animated GIFs from animating in the grid.
	if format != "jpeg" && format != "png" {
		return errImageThumbnailSkipped
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > imageThumbnailMaxPixels {
		return errImageThumbnailSkipped
	}
	width, height := fitWithin(config.Width, config.Height, imageThumbnailMaxWidth, imageThumbnailMaxHeight)
	if width == config.Width && height == config.Height && info.Size() <= imageThumbnailSkipBytes {
		return errImageThumbnailSkipped
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if format == "jpeg" {
		// Browsers rotate a JPEG by its EXIF orientation; a re-encoded
		// thumbnail has no EXIF and would show sideways next to the original.
		if orientation := jpegOrientation(bufio.NewReader(file)); orientation > 1 {
			return errImageThumbnailSkipped
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return err
		}
	}
	source, _, err := image.Decode(bufio.NewReader(file))
	if err != nil {
		return errImageThumbnailSkipped
	}

	thumbnail := downscaleOnto(source, width, height, imageThumbnailBackground)

	// Encode beside the target and rename, so a crash or a concurrent reader
	// never sees a half-written thumbnail.
	temp, err := os.CreateTemp(filepath.Dir(outputPath), ".thumb-*.tmp")
	if err != nil {
		return err
	}
	writer := bufio.NewWriter(temp)
	encodeErr := jpeg.Encode(writer, thumbnail, &jpeg.Options{Quality: imageThumbnailQuality})
	if encodeErr == nil {
		encodeErr = writer.Flush()
	}
	closeErr := temp.Close()
	if err := errors.Join(encodeErr, closeErr); err != nil {
		_ = os.Remove(temp.Name())
		return err
	}
	if err := os.Chmod(temp.Name(), 0o644); err != nil {
		_ = os.Remove(temp.Name())
		return err
	}
	if err := os.Rename(temp.Name(), outputPath); err != nil {
		_ = os.Remove(temp.Name())
		return err
	}
	return nil
}

// fitWithin scales width x height down, keeping the aspect ratio, until it
// fits inside maxWidth x maxHeight. It never scales up.
func fitWithin(width, height, maxWidth, maxHeight int) (int, int) {
	outWidth, outHeight := width, height
	if outWidth > maxWidth {
		outHeight = max(1, int(int64(outHeight)*int64(maxWidth)/int64(outWidth)))
		outWidth = maxWidth
	}
	if outHeight > maxHeight {
		outWidth = max(1, int(int64(outWidth)*int64(maxHeight)/int64(outHeight)))
		outHeight = maxHeight
	}
	return outWidth, outHeight
}

// downscaleOnto flattens source over background and box-filters it down to
// width x height: every output pixel is the average of the source pixels it
// covers, which is what keeps downscaled text and line art from aliasing.
func downscaleOnto(source image.Image, width, height int, background color.RGBA) *image.RGBA {
	bounds := source.Bounds()
	srcWidth, srcHeight := bounds.Dx(), bounds.Dy()

	flat := image.NewRGBA(image.Rect(0, 0, srcWidth, srcHeight))
	draw.Draw(flat, flat.Bounds(), &image.Uniform{C: background}, image.Point{}, draw.Src)
	draw.Draw(flat, flat.Bounds(), source, bounds.Min, draw.Over)
	if width == srcWidth && height == srcHeight {
		return flat
	}

	out := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		y0 := y * srcHeight / height
		y1 := max((y+1)*srcHeight/height, y0+1)
		for x := 0; x < width; x++ {
			x0 := x * srcWidth / width
			x1 := max((x+1)*srcWidth/width, x0+1)
			var r, g, b, count uint64
			for sy := y0; sy < y1; sy++ {
				row := flat.Pix[sy*flat.Stride+x0*4 : sy*flat.Stride+x1*4]
				for i := 0; i < len(row); i += 4 {
					r += uint64(row[i])
					g += uint64(row[i+1])
					b += uint64(row[i+2])
				}
				count += uint64(x1 - x0)
			}
			offset := y*out.Stride + x*4
			out.Pix[offset] = uint8(r / count)
			out.Pix[offset+1] = uint8(g / count)
			out.Pix[offset+2] = uint8(b / count)
			out.Pix[offset+3] = 0xff
		}
	}
	return out
}

// jpegOrientation returns the EXIF orientation (1-8) of a JPEG, or 0 when it
// has none or cannot be read. It walks the segment headers up to the first
// scan and reads tag 0x0112 from IFD0 of an Exif APP1 segment.
func jpegOrientation(reader io.Reader) int {
	var marker [2]byte
	if _, err := io.ReadFull(reader, marker[:]); err != nil || marker[0] != 0xff || marker[1] != 0xd8 {
		return 0
	}
	for {
		if _, err := io.ReadFull(reader, marker[:]); err != nil || marker[0] != 0xff {
			return 0
		}
		if marker[1] == 0xda || marker[1] == 0xd9 { // start of scan / end of image
			return 0
		}
		var size [2]byte
		if _, err := io.ReadFull(reader, size[:]); err != nil {
			return 0
		}
		length := int(binary.BigEndian.Uint16(size[:])) - 2
		if length < 0 {
			return 0
		}
		segment := make([]byte, length)
		if _, err := io.ReadFull(reader, segment); err != nil {
			return 0
		}
		if marker[1] != 0xe1 || len(segment) < 14 || string(segment[:6]) != "Exif\x00\x00" {
			continue
		}
		return tiffOrientation(segment[6:])
	}
}

// tiffOrientation reads the orientation tag from the first IFD of a TIFF blob.
func tiffOrientation(tiff []byte) int {
	if len(tiff) < 8 {
		return 0
	}
	var order binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 0
	}
	offset := int(order.Uint32(tiff[4:8]))
	if offset < 8 || offset+2 > len(tiff) {
		return 0
	}
	entries := int(order.Uint16(tiff[offset : offset+2]))
	for i := 0; i < entries; i++ {
		start := offset + 2 + i*12
		if start+12 > len(tiff) {
			return 0
		}
		if order.Uint16(tiff[start:start+2]) != 0x0112 {
			continue
		}
		value := int(order.Uint16(tiff[start+8 : start+10]))
		if value < 1 || value > 8 {
			return 0
		}
		return value
	}
	return 0
}
