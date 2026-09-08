package client

import (
	"compress/gzip"
	"io"
	"mime"
	"net/http"
	"strings"
	"sync"
)

// gzipWriterPool recycles the (relatively expensive) gzip.Writer allocations
// across requests. BestSpeed keeps the CPU cost per response low; the static
// text assets still shrink 4-6x at that level.
var gzipWriterPool = sync.Pool{
	New: func() any {
		w, _ := gzip.NewWriterLevel(io.Discard, gzip.BestSpeed)
		return w
	},
}

// compressibleTypes is an allow-list of response content types worth gzipping.
// It is deliberately limited to the large, static, non-sensitive text assets
// (the JS bundle, the stylesheet, the app shell HTML, the manifest, SVG). It
// intentionally excludes application/json: the dynamic, per-session API
// responses are small and gzipping attacker-influenced authenticated bodies is
// a needless BREACH-style exposure for no real payload win.
var compressibleTypes = map[string]struct{}{
	"text/html":                 {},
	"text/css":                  {},
	"text/plain":                {},
	"text/javascript":           {},
	"application/javascript":    {},
	"application/manifest+json": {},
	"image/svg+xml":             {},
	"application/xml":           {},
	"text/xml":                  {},
}

// AssetCompression gzips eligible static text responses when the client asks for
// it. It is a no-op for anything else.
//
// Protected user media (/uploads/, /thumbnails/) is never routed through the
// compressor: those bytes are already compressed formats, and keeping the
// wrapper away from them guarantees this layer can never buffer, sniff, or
// alter a private upload response. Uploads reach the network only as opaque
// pass-through bytes (or, for a deliberately shared item, via a share link).
func AssetCompression(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet ||
			r.Header.Get("Range") != "" ||
			!clientAcceptsGzip(r) ||
			isProtectedAssetPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		gz := &gzipResponseWriter{ResponseWriter: w}
		defer gz.close()
		next.ServeHTTP(gz, r)
	})
}

// isProtectedAssetPath reports whether a request targets private user media that
// must bypass the compressor entirely.
func isProtectedAssetPath(path string) bool {
	return strings.HasPrefix(path, "/uploads/") || strings.HasPrefix(path, "/thumbnails/")
}

func clientAcceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		fields := strings.Split(strings.TrimSpace(part), ";")
		if strings.EqualFold(strings.TrimSpace(fields[0]), "gzip") {
			for _, param := range fields[1:] {
				if strings.EqualFold(strings.TrimSpace(param), "q=0") {
					return false
				}
			}
			return true
		}
	}
	return false
}

type gzipResponseWriter struct {
	http.ResponseWriter
	gz          *gzip.Writer
	wroteHeader bool
	compress    bool
}

// Unwrap lets http.ResponseController reach the underlying writer for deadlines
// and flush support.
func (w *gzipResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *gzipResponseWriter) WriteHeader(status int) {
	if w.wroteHeader {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	w.wroteHeader = true

	header := w.ResponseWriter.Header()
	// Only compress a plain 200 with an allow-listed text type that is not
	// already encoded. 304/redirects/partial responses are left untouched.
	if status == http.StatusOK && header.Get("Content-Encoding") == "" && isCompressibleType(header.Get("Content-Type")) {
		w.compress = true
		header.Del("Content-Length")
		header.Del("Accept-Ranges")
		header.Set("Content-Encoding", "gzip")
		addVary(header, "Accept-Encoding")

		gz := gzipWriterPool.Get().(*gzip.Writer)
		gz.Reset(w.ResponseWriter)
		w.gz = gz
	}

	w.ResponseWriter.WriteHeader(status)
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if w.compress {
		return w.gz.Write(b)
	}
	return w.ResponseWriter.Write(b)
}

func (w *gzipResponseWriter) Flush() {
	if w.compress && w.gz != nil {
		_ = w.gz.Flush()
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *gzipResponseWriter) close() {
	if w.gz == nil {
		return
	}
	_ = w.gz.Close()
	w.gz.Reset(io.Discard)
	gzipWriterPool.Put(w.gz)
	w.gz = nil
}

func isCompressibleType(contentType string) bool {
	if contentType == "" {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	_, ok := compressibleTypes[strings.ToLower(mediaType)]
	return ok
}

func addVary(header http.Header, value string) {
	for _, existing := range header.Values("Vary") {
		for _, token := range strings.Split(existing, ",") {
			if strings.EqualFold(strings.TrimSpace(token), value) {
				return
			}
		}
	}
	header.Add("Vary", value)
}
