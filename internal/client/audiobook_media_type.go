package client

import (
	"mime"
	"path/filepath"
	"strings"
)

// audiobookSniffLooksLikeMedia reports whether a sniffed Content-Type is one
// an audio file could plausibly produce. http.DetectContentType only knows a
// handful of audio signatures (an MP3 without an ID3 header, AAC, FLAC and
// friends all come back as application/octet-stream), so this is a denylist
// of things that are definitely not audio rather than an allowlist: anything
// textual (which includes HTML, XML, SVG and scripts), images, PDFs, archives
// and fonts.
func audiobookSniffLooksLikeMedia(sniffed string) bool {
	base := mediaTypeBase(sniffed)
	switch {
	case base == "":
		return false
	case strings.HasPrefix(base, "text/"),
		strings.HasPrefix(base, "image/"),
		strings.HasPrefix(base, "font/"),
		strings.Contains(base, "html"),
		strings.Contains(base, "xml"),
		strings.Contains(base, "javascript"),
		strings.Contains(base, "json"),
		base == "application/pdf",
		base == "application/zip",
		base == "application/x-gzip",
		base == "application/x-rar-compressed",
		base == "application/vnd.ms-fontobject",
		base == "application/postscript",
		base == "application/wasm":
		return false
	}
	return true
}

// audiobookServableMimeType narrows whatever an audiobook's bytes sniffed as
// to a Content-Type that browsers will only ever play or download, never
// render as a document. A generic sniff is upgraded from the file extension
// when that names an audio type, so an MP3 without an ID3 header still plays.
func audiobookServableMimeType(sniffed, filename string) string {
	base := mediaTypeBase(sniffed)
	if audiobookPlayableMimeType(base) {
		return base
	}
	if byExt := mediaTypeBase(mime.TypeByExtension(strings.ToLower(filepath.Ext(filename)))); strings.HasPrefix(byExt, "audio/") {
		return byExt
	}
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".m4b", ".m4a":
		return "audio/mp4"
	case ".mp3":
		return "audio/mpeg"
	}
	return "application/octet-stream"
}

// audiobookPlayableMimeType is the allowlist the media endpoint serves as-is.
// Anything else - including a hostile type stored before upload validation
// existed - goes out as application/octet-stream.
func audiobookPlayableMimeType(base string) bool {
	switch {
	case strings.HasPrefix(base, "audio/"):
		return true
	case base == "video/mp4", base == "video/webm", base == "application/ogg":
		return true
	}
	return false
}

func mediaTypeBase(value string) string {
	base, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(value)), ";")
	return strings.TrimSpace(base)
}
