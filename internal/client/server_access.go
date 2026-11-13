package client

import (
	"io"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strings"

	"memeindex/internal/accessor"
	"memeindex/internal/manager"
)

// writeSessionUnavailable answers an API or asset request whose session could
// not be checked because the user store is down. It is a 503, not a 401, so
// clients keep their session and retry instead of sending the user to log in.
func writeSessionUnavailable(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "5")
	writeJSON(w, http.StatusServiceUnavailable, map[string]any{
		"error": "session check temporarily unavailable, please retry",
	})
}

// writeSessionUnavailablePage is writeSessionUnavailable for page requests.
func writeSessionUnavailablePage(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "5")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = io.WriteString(w, `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><meta http-equiv="refresh" content="5"><title>Temporarily unavailable</title></head><body style="font-family:system-ui,sans-serif;max-width:32rem;margin:4rem auto;padding:0 1rem"><h1>Temporarily unavailable</h1><p>MemeIndex couldn't check your sign-in just now. You are still signed in; this page will retry in a few seconds.</p></body></html>`)
}

func (s *Server) withPageAuth(next http.Handler) http.Handler {
	if !s.auth.enabled() {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, ok, err := s.auth.resolveSession(r)
		if err != nil {
			writeSessionUnavailablePage(w)
			return
		}
		if !ok && allowsAnonymousLinkPreview(r) {
			next.ServeHTTP(w, r)
			return
		}
		if !ok {
			loginURL := "/auth/login?return_to=" + url.QueryEscape(r.URL.RequestURI())
			http.Redirect(w, r, loginURL, http.StatusFound)
			return
		}
		if !session.Permissions.CanView && !session.Permissions.CanViewAudiobooks && !allowsAuthenticatedShellOnly(r.URL.Path) {
			http.Redirect(w, r, "/forbidden", http.StatusFound)
			return
		}

		next.ServeHTTP(w, r.WithContext(contextWithSession(r.Context(), session)))
	})
}

func (s *Server) withProtectedAssetAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setProtectedAssetHeaders(w)
		setUntrustedAssetHeaders(w, r.URL.Path)
		// http.FileServer would otherwise answer a directory path with a
		// listing of every stored file.
		if strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}
		if !s.auth.enabled() {
			allowRevalidatedBrowserCache(w)
			next.ServeHTTP(w, r)
			return
		}

		session, ok, err := s.auth.resolveSession(r)
		if err != nil {
			writeSessionUnavailable(w)
			return
		}
		if !ok || !s.auth.authorizeAssetRequest(session, r) {
			http.NotFound(w, r)
			return
		}
		// A meme someone asked to delete is hidden from the app; stop serving
		// its file and thumbnail too, so it is not still one copied URL away
		// while the request waits for approval. Admins keep access because the
		// approval queue previews exactly these files.
		if !session.Permissions.CanManageUsers && s.managers.IsMemeHidden(assetMemeID(r.URL.Path)) {
			http.NotFound(w, r)
			return
		}

		allowRevalidatedBrowserCache(w)
		next.ServeHTTP(w, r.WithContext(contextWithSession(r.Context(), session)))
	})
}

// allowRevalidatedBrowserCache lets the viewer's own browser keep a meme file
// or thumbnail it was just authorized to fetch, on the condition that it asks
// the server again before every reuse. That request runs the session,
// permission and hidden-meme checks above as usual, so access that has been
// revoked still stops at once; an unchanged file is answered with a bodyless
// 304 instead of being downloaded again on every visit. Shared caches (CDNs,
// proxies) stay on no-store via the CDN headers, and refusals keep no-store
// because this is only called on the path that serves the file.
func allowRevalidatedBrowserCache(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Del("Pragma")
}

// assetMemeID maps /uploads/<id>.<ext> or /thumbnails/<id>.jpg to <id>.
func assetMemeID(requestPath string) string {
	base := path.Base(requestPath)
	return strings.TrimSuffix(base, path.Ext(base))
}

// noDirectoryListing wraps a file server so a directory path 404s instead of
// listing its contents.
func noDirectoryListing(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func setUntrustedAssetHeaders(w http.ResponseWriter, requestPath string) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if !strings.HasPrefix(requestPath, "/uploads/") {
		return
	}
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'; style-src 'unsafe-inline'")
	switch strings.ToLower(filepath.Ext(requestPath)) {
	case ".html", ".htm", ".xhtml", ".svg", ".xml", ".js", ".mjs", ".css", ".url", ".exe", ".dll", ".msi", ".bat", ".cmd", ".ps1", ".sh":
		w.Header().Set("Content-Disposition", "attachment")
		// The CSP above only governs the upload when it is opened as a page.
		// Loaded as a subresource of some other same-origin page (a <script>
		// or <link rel=stylesheet>), what matters is the Content-Type, so
		// never hand out a type the browser would execute or apply - with
		// nosniff, an opaque type is refused as a script or stylesheet.
		// ServeContent keeps a Content-Type that is already set. SVG keeps its
		// image type so <img> previews still render; an image context never
		// runs script, and opening it directly hits the attachment + sandbox.
		if strings.ToLower(filepath.Ext(requestPath)) != ".svg" {
			w.Header().Set("Content-Type", "application/octet-stream")
		}
	}
	if scriptableContentType(w.Header().Get("Content-Type")) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", "attachment")
	}
}

// scriptableContentType reports whether a Content-Type would let a browser run
// or apply the body as code (script, stylesheet, markup) when it is loaded
// from another page on this origin.
func scriptableContentType(value string) bool {
	base, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(value)), ";")
	base = strings.TrimSpace(base)
	switch {
	case base == "", base == "image/svg+xml":
		return false
	case strings.Contains(base, "javascript"), strings.Contains(base, "ecmascript"),
		strings.Contains(base, "html"), strings.Contains(base, "xml"),
		base == "text/css", base == "application/wasm":
		return true
	}
	return false
}

func setProtectedAssetHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "private, no-store, max-age=0")
	w.Header().Set("CDN-Cache-Control", "private, no-store, max-age=0")
	w.Header().Set("Cloudflare-CDN-Cache-Control", "private, no-store, max-age=0")
	w.Header().Set("Surrogate-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Vary", "Cookie")
}

func allowsAnonymousLinkPreview(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	if !isLinkPreviewRequest(r) {
		return false
	}

	switch r.URL.Path {
	case "/":
		return true
	case "/og-image.svg":
		return true
	default:
		return false
	}
}

// rewriteAssetPathsInResponses reports whether outbound meme asset paths need to
// be transformed before serialization (the seam a future signed / expiring URL
// scheme would hook into). While it is false every protect*ForResponse helper is
// a pass-through and skips the per-response slice copy and struct duplication
// entirely - which is the common case on the hot list/dashboard endpoints.
func (s *Server) rewriteAssetPathsInResponses() bool { return false }

func (s *Server) protectAssetPathForResponse(r *http.Request, assetPath string) string {
	return assetPath
}

func (s *Server) protectMemeForResponse(r *http.Request, meme accessor.Meme) accessor.Meme {
	if !s.rewriteAssetPathsInResponses() {
		return meme
	}
	meme.FilePath = s.protectAssetPathForResponse(r, meme.FilePath)
	meme.PreviewPath = s.protectAssetPathForResponse(r, meme.PreviewPath)
	return meme
}

func (s *Server) protectMemesForResponse(r *http.Request, memes []accessor.Meme) []accessor.Meme {
	if !s.rewriteAssetPathsInResponses() || len(memes) == 0 {
		return memes
	}

	protected := make([]accessor.Meme, len(memes))
	for i := range memes {
		protected[i] = s.protectMemeForResponse(r, memes[i])
	}
	return protected
}

func (s *Server) protectMemeListForResponse(r *http.Request, result manager.MemeListResult) manager.MemeListResult {
	if !s.rewriteAssetPathsInResponses() || len(result.Memes) == 0 {
		return result
	}

	protected := result
	protected.Memes = s.protectMemesForResponse(r, result.Memes)
	return protected
}

func (s *Server) protectPendingDeletesForResponse(r *http.Request, records accessor.PagedPendingDeletes) accessor.PagedPendingDeletes {
	if !s.rewriteAssetPathsInResponses() || len(records.Memes) == 0 {
		return records
	}

	protected := records
	protected.Memes = make([]accessor.PendingDeleteRecord, len(records.Memes))
	for i := range records.Memes {
		protected.Memes[i] = records.Memes[i]
		protected.Memes[i].Meme = s.protectMemeForResponse(r, records.Memes[i].Meme)
	}
	return protected
}

func (s *Server) protectAuditFeedForResponse(r *http.Request, feed accessor.PagedAuditFeed) accessor.PagedAuditFeed {
	if !s.rewriteAssetPathsInResponses() || len(feed.Events) == 0 {
		return feed
	}

	protected := feed
	protected.Events = make([]accessor.GlobalMemeAuditEntry, len(feed.Events))
	for i := range feed.Events {
		protected.Events[i] = feed.Events[i]
		protected.Events[i].MemeFilePath = s.protectAssetPathForResponse(r, feed.Events[i].MemeFilePath)
	}
	return protected
}

func allowsAuthenticatedShellOnly(path string) bool {
	switch path {
	case "/forbidden":
		return true
	default:
		return false
	}
}

func (s *Server) withAPIAuth(next http.Handler, minimum permissionLevel) http.Handler {
	if !s.auth.enabled() {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, ok, err := s.auth.resolveSession(r)
		if err != nil {
			writeSessionUnavailable(w)
			return
		}
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]any{
				"error": "authentication required",
			})
			return
		}

		if !hasPermission(session.Permissions, minimum) {
			writeJSON(w, http.StatusForbidden, map[string]any{
				"error": "insufficient permissions",
			})
			return
		}

		next.ServeHTTP(w, r.WithContext(contextWithSession(r.Context(), session)))
	})
}

func hasPermission(permissions authPermissions, minimum permissionLevel) bool {
	switch minimum {
	case permissionManageUsers:
		return permissions.CanManageUsers
	case permissionDeleteMemes:
		return permissions.CanDeleteMemes || permissions.CanManageUsers
	case permissionMetadata:
		return permissions.CanAddTags || permissions.CanRemoveTags || permissions.CanManageUsers
	case permissionUpload:
		return permissions.CanUpload || permissions.CanManageUsers
	case permissionViewAudiobooks:
		return permissions.CanViewAudiobooks || permissions.CanUploadAudiobooks || permissions.CanDeleteAudiobooks || permissions.CanManageUsers
	case permissionUploadAudiobooks:
		return permissions.CanUploadAudiobooks || permissions.CanManageUsers
	case permissionDeleteAudiobooks:
		return permissions.CanDeleteAudiobooks || permissions.CanManageUsers
	case permissionManageAudiobookSeries:
		return permissions.CanManageAudiobookSeries || permissions.CanManageUsers
	default:
		return permissions.CanView || permissions.CanUpload || permissions.CanManage
	}
}

func (s *Server) requirePermission(w http.ResponseWriter, r *http.Request, minimum permissionLevel) bool {
	if !s.auth.enabled() {
		return true
	}

	session, ok := sessionFromContext(r.Context())
	if !ok || !hasPermission(session.Permissions, minimum) {
		writeJSON(w, http.StatusForbidden, map[string]any{
			"error": "insufficient permissions",
		})
		return false
	}
	return true
}
