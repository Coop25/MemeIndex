package client

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"html"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"memeindex/internal/accessor"
)

const memeShareLifetime = 30 * 24 * time.Hour

func loadOrCreateShareSecret(dataDir, configured string) ([]byte, error) {
	if strings.TrimSpace(configured) != "" {
		digest := sha256.Sum256([]byte(strings.TrimSpace(configured)))
		return digest[:], nil
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	secretPath := filepath.Join(dataDir, "share_secret")
	if payload, err := os.ReadFile(secretPath); err == nil {
		secret, decodeErr := base64.RawURLEncoding.DecodeString(strings.TrimSpace(string(payload)))
		if decodeErr != nil || len(secret) < 32 {
			return nil, errors.New("stored share secret is invalid")
		}
		return secret, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	if err := os.WriteFile(secretPath, []byte(base64.RawURLEncoding.EncodeToString(secret)), 0o600); err != nil {
		return nil, err
	}
	return secret, nil
}

func (s *Server) createMemeShare(w http.ResponseWriter, r *http.Request, memeID string) {
	if s.enforceRateLimit(w, r, s.writeLimiter, "share") {
		return
	}
	now := time.Now().UTC()
	share, err := s.managers.GetOrCreateMemeShare(memeID, currentUserID(r), now)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "failed to create share link", http.StatusInternalServerError)
		return
	}
	_, meme, err := s.managers.GetMemeShare(memeID)
	if err != nil {
		http.Error(w, "failed to load shared meme", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"url":        s.memeSharePageURL(r, share),
		"page_url":   s.memeSharePageURL(r, share),
		"media_url":  s.memeShareMediaURL(r, share, meme),
		"expires_at": share.ExpiresAt,
	})
}

func (s *Server) handleMemeLink(w http.ResponseWriter, r *http.Request) {
	// Every response depends on current share validity, including redirects.
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	w.Header().Set("CDN-Cache-Control", "no-store")
	w.Header().Set("Cloudflare-CDN-Cache-Control", "no-store")
	w.Header().Set("Surrogate-Control", "no-store")
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	trimmed := strings.Trim(strings.TrimPrefix(r.URL.Path, "/m/"), "/")
	parts := strings.Split(trimmed, "/")
	if len(parts) == 0 || parts[0] == "" || len(parts) > 3 {
		http.NotFound(w, r)
		return
	}
	memeID := parts[0]
	assetKind := ""
	if len(parts) >= 2 {
		assetKind = parts[1]
		if assetKind != "media" && assetKind != "preview" {
			http.NotFound(w, r)
			return
		}
	}

	token := strings.TrimSpace(r.URL.Query().Get("share"))
	if token != "" {
		share, meme, err := s.managers.GetMemeShare(memeID)
		if err == nil && s.validMemeShareToken(token, share, time.Now().UTC()) {
			if len(parts) == 3 && parts[2] != sharedAssetFileName(meme, assetKind == "preview") {
				http.NotFound(w, r)
				return
			}
			if assetKind == "media" {
				s.serveSharedMemeAsset(w, r, meme, false)
				return
			}
			if assetKind == "preview" {
				s.serveSharedMemeAsset(w, r, meme, true)
				return
			}
			// Every client - browsers, Discordbot, and Discord's media proxy,
			// which fetches later under a different User-Agent - gets the same
			// page. Its OpenGraph tags point at the /media/ URL, which serves
			// the file regardless of who asks, so the embed no longer depends
			// on User-Agent sniffing.
			s.renderSharedMemePage(w, r, token, meme, share.ExpiresAt)
			return
		}
		if assetKind == "" {
			http.Redirect(w, r, "/m/"+url.PathEscape(memeID), http.StatusFound)
			return
		}
	}

	if assetKind != "" {
		if len(parts) == 3 {
			http.Redirect(w, r, "/m/"+url.PathEscape(memeID), http.StatusFound)
			return
		}
		http.NotFound(w, r)
		return
	}
	s.withPageAuth(http.HandlerFunc(s.handleIndex)).ServeHTTP(w, r)
}

func (s *Server) handleAdminShares(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/admin/shares" {
		if r.Method == http.MethodDelete {
			revoked, err := s.managers.RevokeAllMemeShares(time.Now().UTC())
			if err != nil {
				http.Error(w, "failed to revoke shares", http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"revoked": revoked})
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		shares, err := s.managers.ListActiveMemeShares(time.Now().UTC())
		if err != nil {
			http.Error(w, "failed to list shared memes", http.StatusInternalServerError)
			return
		}
		displayNames := map[string]string{}
		if s.users != nil {
			if users, listErr := s.users.ListUsers(r.Context()); listErr == nil {
				for _, user := range users {
					if name := strings.TrimSpace(user.DisplayName); name != "" {
						displayNames[strings.TrimSpace(user.UserID)] = name
					}
				}
			}
		}
		items := make([]map[string]any, 0, len(shares))
		for _, entry := range shares {
			sharedByID := strings.TrimSpace(entry.Share.SharedByUserID)
			sharedByDisplayName := displayNames[sharedByID]
			if sharedByDisplayName == "" {
				sharedByDisplayName = sharedByID
			}
			if sharedByDisplayName == "" {
				sharedByDisplayName = "Local user"
			}
			items = append(items, map[string]any{
				"meme":                   s.protectMemeForResponse(r, entry.Meme),
				"share":                  entry.Share,
				"shared_by_display_name": sharedByDisplayName,
				"url":                    s.memeSharePageURL(r, entry.Share),
				"page_url":               s.memeSharePageURL(r, entry.Share),
				"media_url":              s.memeShareMediaURL(r, entry.Share, entry.Meme),
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"shares": items})
		return
	}

	memeID := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/admin/shares/"), "/")
	if memeID == "" || strings.Contains(memeID, "/") {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := s.managers.RevokeMemeShare(memeID); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "failed to revoke share", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) memeSharePageURL(r *http.Request, share accessor.MemeShareState) string {
	return s.publicOrigin(r) + "/m/" + url.PathEscape(share.MemeID) + "?share=" + url.QueryEscape(s.signMemeShare(share))
}

func (s *Server) memeShareMediaURL(r *http.Request, share accessor.MemeShareState, meme accessor.Meme) string {
	return s.publicOrigin(r) + "/m/" + url.PathEscape(share.MemeID) + "/media/" + url.PathEscape(sharedAssetFileName(meme, false)) + "?share=" + url.QueryEscape(s.signMemeShare(share))
}

// publicOrigin returns the origin shared links should use. Memes can be
// reached from a public domain (reverse-proxied Addr listener) or from the
// direct-LAN LocalAddr listener, whose Host header carries a local IP that
// is meaningless to whoever the link is shared with. When a public base URL
// is configured, every shared link uses it regardless of which listener
// served the request that created the share.
func (s *Server) publicOrigin(r *http.Request) string {
	if s.config.PublicBaseURL != "" {
		return s.config.PublicBaseURL
	}
	return requestOrigin(r)
}

func (s *Server) signMemeShare(share accessor.MemeShareState) string {
	expires := strconv.FormatInt(share.ExpiresAt.UTC().Unix(), 10)
	generation := strconv.FormatInt(share.Generation, 10)
	payload := share.MemeID + "\n" + expires + "\n" + generation
	mac := hmac.New(sha256.New, s.shareSecret)
	_, _ = mac.Write([]byte(payload))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return expires + "." + generation + "." + signature
}

func (s *Server) validMemeShareToken(token string, share accessor.MemeShareState, now time.Time) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || len(s.shareSecret) == 0 || !share.ExpiresAt.After(now) {
		return false
	}
	expires, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || expires != share.ExpiresAt.UTC().Unix() || now.Unix() >= expires {
		return false
	}
	generation, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || generation != share.Generation {
		return false
	}
	expected := s.signMemeShare(share)
	return hmac.Equal([]byte(expected), []byte(token))
}

func (s *Server) serveSharedMemeAsset(w http.ResponseWriter, r *http.Request, meme accessor.Meme, preview bool) {
	fileName := meme.StoredName
	baseDir := s.managers.UploadDir()
	contentType := strings.TrimSpace(meme.ContentType)
	downloadName := strings.TrimSpace(meme.OriginalName)
	if preview && meme.PreviewPath != "" && strings.HasPrefix(meme.PreviewPath, "/thumbnails/") {
		fileName = filepath.Base(meme.PreviewPath)
		baseDir = s.managers.ThumbnailDir()
		contentType = mime.TypeByExtension(filepath.Ext(fileName))
		downloadName = "preview" + filepath.Ext(fileName)
	}
	if strings.TrimSpace(fileName) == "" || strings.TrimSpace(baseDir) == "" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	w.Header().Set("CDN-Cache-Control", "no-store")
	w.Header().Set("Cloudflare-CDN-Cache-Control", "no-store")
	w.Header().Set("Surrogate-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Accept-Ranges", "bytes")
	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	if downloadName != "" {
		if disposition := mime.FormatMediaType("inline", map[string]string{"filename": downloadName}); disposition != "" {
			w.Header().Set("Content-Disposition", disposition)
		}
	}
	setUntrustedAssetHeaders(w, "/uploads/"+fileName)
	http.ServeFile(w, r, filepath.Join(baseDir, fileName))
}

func (s *Server) renderSharedMemePage(w http.ResponseWriter, r *http.Request, token string, meme accessor.Meme, expiresAt time.Time) {
	origin := s.publicOrigin(r)
	shareURL := origin + "/m/" + url.PathEscape(meme.ID) + "?share=" + url.QueryEscape(token)
	mediaURL := origin + "/m/" + url.PathEscape(meme.ID) + "/media/" + url.PathEscape(sharedAssetFileName(meme, false)) + "?share=" + url.QueryEscape(token)
	previewURL := origin + "/m/" + url.PathEscape(meme.ID) + "/preview/" + url.PathEscape(sharedAssetFileName(meme, true)) + "?share=" + url.QueryEscape(token)
	openURL := "/m/" + url.PathEscape(meme.ID)
	title := "Shared meme from MemeIndex"
	mediaTag := `<a class="file" href="` + html.EscapeString(mediaURL) + `">Open shared file</a>`
	ogType := "website"
	// Shares are reused until they expire, so the time left varies.
	description := "Shared privately until " + expiresAt.UTC().Format("January 2, 2006") + "."
	// No og:image fallback for other file types: Discord does not render SVG
	// (the old /og-image.svg fallback), so a generic image only added noise.
	meta := ""
	ogTag := func(property, content string) string {
		return `<meta property="` + property + `" content="` + html.EscapeString(content) + `">`
	}
	twitterTag := func(name, content string) string {
		return `<meta name="` + name + `" content="` + html.EscapeString(content) + `">`
	}
	sizeTags := func(prefix string, width, height int) string {
		if width <= 0 || height <= 0 {
			return ""
		}
		return ogTag(prefix+":width", strconv.Itoa(width)) + ogTag(prefix+":height", strconv.Itoa(height))
	}
	switch {
	case strings.HasPrefix(meme.ContentType, "image/"):
		width, height := s.sharedImageDimensions(meme, false)
		meta = ogTag("og:image", mediaURL) + ogTag("og:image:secure_url", mediaURL) + ogTag("og:image:type", meme.ContentType) + sizeTags("og:image", width, height) + ogTag("og:image:alt", "Shared meme") +
			twitterTag("twitter:card", "summary_large_image") + twitterTag("twitter:image", mediaURL) + twitterTag("twitter:image:alt", "Shared meme")
		mediaTag = `<img src="` + html.EscapeString(mediaURL) + `" alt="Shared meme">`
	case strings.HasPrefix(meme.ContentType, "video/"):
		ogType = "video.other"
		// The poster frame shares the video's aspect ratio, so its size stands
		// in for the video's; Discord needs a size to draw an inline player.
		width, height := s.sharedImageDimensions(meme, true)
		meta = ogTag("og:video", mediaURL) + ogTag("og:video:url", mediaURL) + ogTag("og:video:secure_url", mediaURL) + ogTag("og:video:type", meme.ContentType) + sizeTags("og:video", width, height)
		if strings.HasPrefix(meme.PreviewPath, "/thumbnails/") {
			meta += ogTag("og:image", previewURL) + ogTag("og:image:secure_url", previewURL) + ogTag("og:image:type", mime.TypeByExtension(filepath.Ext(sharedAssetFileName(meme, true)))) + sizeTags("og:image", width, height) + ogTag("og:image:alt", "Shared meme preview") +
				twitterTag("twitter:card", "summary_large_image") + twitterTag("twitter:image", previewURL)
		}
		mediaTag = `<video src="` + html.EscapeString(mediaURL) + `" controls autoplay loop playsinline></video>`
	case strings.HasPrefix(meme.ContentType, "audio/"):
		meta = ogTag("og:audio", mediaURL) + ogTag("og:audio:secure_url", mediaURL) + ogTag("og:audio:type", meme.ContentType)
		mediaTag = `<audio src="` + html.EscapeString(mediaURL) + `" controls autoplay></audio>`
	}

	page := `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>` + title + `</title><meta name="robots" content="noindex,nofollow,noarchive">` + ogTag("og:type", ogType) + `<meta property="og:site_name" content="MemeIndex"><meta property="og:title" content="` + title + `">` + ogTag("og:description", description) + ogTag("og:url", shareURL) + `<meta name="twitter:title" content="` + title + `">` + twitterTag("twitter:description", description) + `` + meta + `<style>html,body{margin:0;min-height:100%;background:#070b0a;color:#eef7f0;font:16px system-ui}body{display:grid;place-items:center;padding:24px;box-sizing:border-box}.wrap{width:min(1100px,100%);text-align:center}img,video{max-width:100%;max-height:82vh;border-radius:12px;background:#000}audio{width:min(680px,100%)}a{color:#72c77a}.open{display:inline-block;margin-top:18px;padding:11px 16px;border:1px solid #315d3b;border-radius:9px;text-decoration:none}.meta{color:#91a397;font-size:13px}</style></head><body><main class="wrap">` + mediaTag + `<p><a class="open" href="` + html.EscapeString(openURL) + `">Open in MemeIndex</a></p><p class="meta">Public link expires ` + html.EscapeString(expiresAt.Local().Format("January 2, 2006")) + `.</p></main></body></html>`
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self'; media-src 'self'; style-src 'unsafe-inline'; base-uri 'none'; frame-ancestors 'none'")
	_, _ = w.Write([]byte(page))
}

// sharedImageDimensions reads only the header of the meme's image (or, with
// preview, its poster thumbnail) to report its pixel size. It returns zeros
// for formats the standard library cannot decode, in which case the share
// page simply omits the size tags.
func (s *Server) sharedImageDimensions(meme accessor.Meme, preview bool) (int, int) {
	var path string
	if preview {
		if !strings.HasPrefix(meme.PreviewPath, "/thumbnails/") || strings.TrimSpace(s.managers.ThumbnailDir()) == "" {
			return 0, 0
		}
		path = filepath.Join(s.managers.ThumbnailDir(), filepath.Base(meme.PreviewPath))
	} else {
		if strings.TrimSpace(meme.StoredName) == "" {
			return 0, 0
		}
		path = filepath.Join(s.managers.UploadDir(), filepath.Base(meme.StoredName))
	}
	file, err := os.Open(path)
	if err != nil {
		return 0, 0
	}
	defer file.Close()
	config, _, err := image.DecodeConfig(file)
	if err != nil {
		return 0, 0
	}
	return config.Width, config.Height
}

func sharedAssetFileName(meme accessor.Meme, preview bool) string {
	extension := filepath.Ext(meme.OriginalName)
	if preview && strings.HasPrefix(meme.PreviewPath, "/thumbnails/") {
		extension = filepath.Ext(meme.PreviewPath)
	}
	if extension == "" && !preview {
		if extensions, err := mime.ExtensionsByType(meme.ContentType); err == nil && len(extensions) > 0 {
			extension = extensions[0]
		}
	}
	if preview {
		return "preview" + strings.ToLower(extension)
	}
	return "shared" + strings.ToLower(extension)
}

func requestOrigin(r *http.Request) string {
	scheme := forwardedHeaderFirstValue(r.Header.Get("X-Forwarded-Proto"))
	if scheme == "" {
		if r.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}
	// The public proxy preserves Host; arbitrary forwarded hosts are untrusted.
	host := r.Host
	return scheme + "://" + host
}

func isMemeDeepLinkPath(path string) bool {
	trimmed := strings.TrimPrefix(path, "/m/")
	return trimmed != "" && !strings.Contains(trimmed, "/")
}

func safeLocalReturnPath(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || !strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, "//") {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || strings.ContainsAny(parsed.Path, "\\\r\n") || strings.HasPrefix(parsed.Path, "//") {
		return ""
	}
	return parsed.RequestURI()
}
