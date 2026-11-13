package client

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"html"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

func (s *Server) handleWebManifest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/manifest+json")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	http.ServeFile(w, r, filepath.Join("static", "manifest.webmanifest"))
}

func (s *Server) handleServiceWorker(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Service-Worker-Allowed", "/")
	http.ServeFile(w, r, filepath.Join("static", "service-worker.js"))
}

// staticAssetHandler serves the bundled frontend assets. When the request URL
// carries a cache-busting fingerprint (…?v=&h=, produced by buildAssetURL) the
// body for that URL can never change, so it is marked immutable for a year.
// The directive stays "private": the bundle is served behind session auth and
// must not be retained by a shared proxy or CDN.
func staticAssetHandler(dir string) http.Handler {
	fs := http.StripPrefix("/static/", http.FileServer(http.Dir(dir)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}
		if q := r.URL.Query(); q.Get("h") != "" || q.Get("v") != "" {
			w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
		} else {
			// No fingerprint means the URL never changes across deploys (the
			// audiobook sync worker, favicons). Without an explicit policy the
			// browser guesses a freshness lifetime from Last-Modified and can
			// keep running an old script next to a new app.js, so make it
			// revalidate; an unchanged file costs one 304.
			w.Header().Set("Cache-Control", "private, no-cache")
		}
		fs.ServeHTTP(w, r)
	})
}

// indexShellState memoises the transformed app-shell HTML. The shell is
// identical for every viewer; only its embedded asset URLs move, and those
// change only when index.html, the JS bundle, the stylesheet, or the build
// version changes. Caching it avoids re-reading and rewriting ~50 KB of HTML on
// every "/" hit and every meme deep link.
var indexShellState struct {
	mu   sync.Mutex
	key  string
	body []byte
}

func renderIndexShell(refreshToken string) ([]byte, error) {
	indexPath := filepath.Join("static", "index.html")
	stylesRel := filepath.Join("static", "styles.css")
	appRel := filepath.Join("static", "app.js")
	adminRel := filepath.Join("static", "admin-pages.js")

	stylesHash := assetContentHash(stylesRel)
	appHash := assetContentHash(appRel)
	adminHash := assetContentHash(adminRel)

	var key string
	if info, err := os.Stat(indexPath); err == nil {
		key = fmt.Sprintf("%d:%d:%s:%s:%s:%s:%s", info.Size(), info.ModTime().UnixNano(), BuildVersion(), stylesHash, appHash, adminHash, refreshToken)
	}

	if key != "" {
		indexShellState.mu.Lock()
		if indexShellState.body != nil && indexShellState.key == key {
			body := indexShellState.body
			indexShellState.mu.Unlock()
			return body, nil
		}
		indexShellState.mu.Unlock()
	}

	content, err := os.ReadFile(indexPath)
	if err != nil {
		return nil, err
	}

	replaced := strings.ReplaceAll(string(content), "/static/styles.css", html.EscapeString(buildAssetURL("/static/styles.css", refreshToken)))
	replaced = strings.ReplaceAll(replaced, "/static/app.js", html.EscapeString(buildAssetURL("/static/app.js", refreshToken)))
	replaced = strings.ReplaceAll(replaced, "/static/admin-pages.js", html.EscapeString(buildAssetURL("/static/admin-pages.js", refreshToken)))
	body := []byte(replaced)

	if key != "" {
		indexShellState.mu.Lock()
		indexShellState.key = key
		indexShellState.body = body
		indexShellState.mu.Unlock()
	}
	return body, nil
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && !isMemeDeepLinkPath(r.URL.Path) {
		http.NotFound(w, r)
		return
	}

	// Always fetch the current shell so it points at the latest JS and CSS.
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	refreshToken := strings.TrimSpace(r.URL.Query().Get("refresh"))

	body, err := renderIndexShell(refreshToken)
	if err != nil {
		http.Error(w, "failed to load index", http.StatusInternalServerError)
		return
	}

	// OpenGraph requires absolute image URLs, and the shell is cached without
	// knowing the origin, so make the preview icon absolute per request. The
	// icon lives under /pwa-icons/, which link-preview crawlers can fetch
	// without signing in (unlike /static/).
	origin := html.EscapeString(s.publicOrigin(r))
	body = bytes.ReplaceAll(body, []byte(`content="/pwa-icons/`), []byte(`content="`+origin+`/pwa-icons/`))

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(body)
}

func (s *Server) handleAccessDeniedPage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/forbidden" {
		http.NotFound(w, r)
		return
	}

	displayName := "Mystery Goblin"
	username := ""
	userID := ""
	if session, ok := sessionFromContext(r.Context()); ok {
		switch {
		case strings.TrimSpace(session.DisplayName) != "":
			displayName = session.DisplayName
		case strings.TrimSpace(session.Username) != "":
			displayName = session.Username
		case strings.TrimSpace(session.UserID) != "":
			displayName = "Discord User " + session.UserID
		}
		username = strings.TrimSpace(session.Username)
		userID = strings.TrimSpace(session.UserID)

		if s.users != nil && userID != "" {
			// Always persist denied visitors so admins can see them with zero permissions.
			if err := s.users.RecordDeniedVisitor(r.Context(), authClaims{
				Subject:     userID,
				Username:    username,
				DisplayName: displayName,
				AvatarURL:   session.AvatarURL,
			}); err != nil {
				log.Printf("persist denied-page user failed for %s: %v", userID, err)
			}
		}
	}

	content, err := os.ReadFile(filepath.Join("static", "access-denied.html"))
	if err != nil {
		http.Error(w, "failed to load access denied page", http.StatusInternalServerError)
		return
	}

	nonce := newCSPNonce()
	page := string(content)
	page = strings.ReplaceAll(page, "{{DISPLAY_NAME}}", html.EscapeString(displayName))
	page = strings.ReplaceAll(page, "{{USERNAME}}", html.EscapeString(username))
	page = strings.ReplaceAll(page, "{{USER_ID}}", html.EscapeString(userID))
	page = strings.ReplaceAll(page, "{{VERSION}}", html.EscapeString(BuildVersion()))
	page = strings.ReplaceAll(page, "{{SCRIPT_NONCE}}", html.EscapeString(nonce))

	w.Header().Set("Cache-Control", "no-store, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	// The page ships one static inline <script> and inline <style>; pin the
	// script to a per-response nonce instead of allowing 'unsafe-inline'.
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'nonce-"+nonce+"'; style-src 'self' 'unsafe-inline'; img-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(page))
}

func buildAssetURL(path string, refreshToken string) string {
	query := url.Values{}
	query.Set("v", BuildVersion())
	// Image tags such as "main" and local builds can retain the same version.
	// Fingerprint the files themselves so frontend edits always get a new URL.
	if hash := assetContentHash(filepath.FromSlash(strings.TrimPrefix(path, "/"))); hash != "" {
		query.Set("h", hash)
	}
	if refreshToken != "" {
		query.Set("r", refreshToken)
	}
	return path + "?" + query.Encode()
}

type assetFingerprint struct {
	modTime time.Time
	size    int64
	hash    string
}

// assetContentCache memoises the short content hash of a static asset keyed by
// its absolute path. handleIndex builds two asset URLs on every "/" request and
// every meme deep link; without this each one re-read and re-hashed the file.
var assetContentCache sync.Map // string -> assetFingerprint

// assetContentHash returns the first-8-bytes hex SHA-256 of a static asset,
// re-reading it only when its size or modification time has changed.
func assetContentHash(relPath string) string {
	key := relPath
	if abs, err := filepath.Abs(relPath); err == nil {
		key = abs
	}

	info, err := os.Stat(key)
	if err != nil {
		return ""
	}
	if cached, ok := assetContentCache.Load(key); ok {
		if fp := cached.(assetFingerprint); fp.size == info.Size() && fp.modTime.Equal(info.ModTime()) {
			return fp.hash
		}
	}

	content, err := os.ReadFile(key)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(content)
	hash := fmt.Sprintf("%x", digest[:8])
	assetContentCache.Store(key, assetFingerprint{modTime: info.ModTime(), size: info.Size(), hash: hash})
	return hash
}

func (s *Server) handleOGImage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" width="1200" height="630" viewBox="0 0 1200 630" role="img" aria-label="MemeIndex">
  <defs>
    <linearGradient id="bg" x1="0%" y1="0%" x2="100%" y2="100%">
      <stop offset="0%" stop-color="#0d1014"/>
      <stop offset="55%" stop-color="#111821"/>
      <stop offset="100%" stop-color="#0c1218"/>
    </linearGradient>
    <linearGradient id="accent" x1="0%" y1="0%" x2="100%" y2="100%">
      <stop offset="0%" stop-color="#3a8ff0"/>
      <stop offset="100%" stop-color="#4cd3d3"/>
    </linearGradient>
  </defs>
  <rect width="1200" height="630" fill="url(#bg)"/>
  <circle cx="1060" cy="110" r="180" fill="#4cd3d3" fill-opacity="0.08"/>
  <circle cx="150" cy="560" r="210" fill="#3a8ff0" fill-opacity="0.10"/>
  <rect x="82" y="82" width="148" height="148" rx="36" fill="url(#accent)"/>
  <text x="156" y="176" text-anchor="middle" font-family="Segoe UI, Arial, sans-serif" font-size="64" font-weight="800" fill="#081015">MI</text>
  <text x="82" y="318" font-family="Segoe UI, Arial, sans-serif" font-size="86" font-weight="800" fill="#f2f5f7">MemeIndex</text>
  <text x="82" y="382" font-family="Segoe UI, Arial, sans-serif" font-size="32" font-weight="600" fill="#4cd3d3" letter-spacing="4">SELF-HOSTED ARCHIVE</text>
  <text x="82" y="470" font-family="Segoe UI, Arial, sans-serif" font-size="34" fill="#c9d4dc">Browse, tag, favorite, and organize your meme backlog.</text>
  <text x="82" y="520" font-family="Segoe UI, Arial, sans-serif" font-size="34" fill="#c9d4dc">Images stay light. Videos can preview with generated thumbnails.</text>
</svg>`))
}

func isLinkPreviewRequest(r *http.Request) bool {
	userAgent := strings.ToLower(strings.TrimSpace(r.Header.Get("User-Agent")))
	if userAgent == "" {
		return false
	}

	linkPreviewAgents := []string{
		"discordbot",
		"slackbot",
		"twitterbot",
		"facebookexternalhit",
		"linkedinbot",
		"whatsapp",
		"telegrambot",
		"skypeuripreview",
		"googlebot",
		"bingbot",
	}

	for _, agent := range linkPreviewAgents {
		if strings.Contains(userAgent, agent) {
			return true
		}
	}

	return false
}
