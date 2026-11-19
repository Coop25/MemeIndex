package accessor

import (
	"mime"
	"path/filepath"
	"regexp"
	"strings"
)

// storedExtensionPattern is what an extension on a stored upload name may look
// like. The stored name becomes the /uploads/<id><ext> URL as-is, so anything
// a URL treats specially (?, #, %, spaces, backslashes) would leave the meme
// with a file and thumbnail link that does not resolve.
var storedExtensionPattern = regexp.MustCompile(`^\.[a-z0-9]{1,10}$`)

// preferredMediaExtensions names the extension to store for the common media
// types, where the MIME table would otherwise pick its alphabetically first
// alias (.jfif for JPEG, for one) and differ between operating systems.
var preferredMediaExtensions = map[string]string{
	"image/jpeg":       ".jpg",
	"image/png":        ".png",
	"image/gif":        ".gif",
	"image/webp":       ".webp",
	"image/avif":       ".avif",
	"image/bmp":        ".bmp",
	"image/heic":       ".heic",
	"image/heif":       ".heif",
	"image/tiff":       ".tiff",
	"video/mp4":        ".mp4",
	"video/webm":       ".webm",
	"video/quicktime":  ".mov",
	"video/x-matroska": ".mkv",
	"video/x-msvideo":  ".avi",
	"video/avi":        ".avi",
	"video/mpeg":       ".mpeg",
	"video/ogg":        ".ogv",
	"audio/mpeg":       ".mp3",
	"audio/mp4":        ".m4a",
	"audio/aac":        ".aac",
	"audio/flac":       ".flac",
	"audio/ogg":        ".ogg",
	"application/ogg":  ".ogg",
	"audio/wav":        ".wav",
	"audio/wave":       ".wav",
	"audio/x-wav":      ".wav",
	"audio/webm":       ".weba",
}

// storedFileExtension picks the extension for a new upload's stored name. The
// filename's own extension is kept, lower-cased, when it is plain letters and
// digits. Otherwise it is dropped, and an extension for the detected media
// type takes its place when there is one, so the file server still serves it
// with the right type. The meme's OriginalName keeps the name as given; only
// the name on disk and in the URL changes.
func storedFileExtension(filename, contentType string) string {
	if ext := strings.ToLower(filepath.Ext(filename)); storedExtensionPattern.MatchString(ext) {
		return ext
	}
	return extensionForMediaType(contentType)
}

// extensionForMediaType returns an extension for an image, video or audio
// type, or "" for anything else. Other types get no extension rather than one
// the browser might treat as a page or a script (.html for a client-claimed
// text/html, say); the file server then sniffs the bytes. SVG does get .svg,
// which /uploads/ always serves as a sandboxed download that still renders in
// an <img>.
func extensionForMediaType(contentType string) string {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return ""
	}
	if ext, ok := preferredMediaExtensions[mediaType]; ok {
		return ext
	}
	if !strings.HasPrefix(mediaType, "image/") && !strings.HasPrefix(mediaType, "video/") && !strings.HasPrefix(mediaType, "audio/") {
		return ""
	}
	extensions, err := mime.ExtensionsByType(mediaType)
	if err != nil {
		return ""
	}
	for _, ext := range extensions {
		if ext = strings.ToLower(ext); storedExtensionPattern.MatchString(ext) {
			return ext
		}
	}
	return ""
}
