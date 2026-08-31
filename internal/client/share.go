package client

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"html"
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
	writeJSON(w, http.StatusCreated, map[string]any{
		"url":        s.memeShareURL(r, share),
		"expires_at": share.ExpiresAt,
	})
}

func (s *Server) handleMemeLink(w http.ResponseWriter, r *http.Request) {
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
			s.renderSharedMemePage(w, r, token, meme, share.ExpiresAt)
			return
		}
		if assetKind == "" {
			http.Redirect(w, r, "/m/"+url.PathEscape(memeID), http.StatusFound)
			return
		}
	}

	if assetKind != "" {
		http.NotFound(w, r)
		return
	}
	s.withPageAuth(http.HandlerFunc(s.handleIndex)).ServeHTTP(w, r)
}

func (s *Server) handleAdminShares(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/admin/shares" {
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
				"url":                    s.memeShareURL(r, entry.Share),
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

func (s *Server) memeShareURL(r *http.Request, share accessor.MemeShareState) string {
	return requestOrigin(r) + "/m/" + url.PathEscape(share.MemeID) + "?share=" + url.QueryEscape(s.signMemeShare(share))
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
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Header().Set("CDN-Cache-Control", "public, max-age=300")
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
	origin := requestOrigin(r)
	shareURL := origin + "/m/" + url.PathEscape(meme.ID) + "?share=" + url.QueryEscape(token)
	mediaURL := origin + "/m/" + url.PathEscape(meme.ID) + "/media/" + url.PathEscape(sharedAssetFileName(meme, false)) + "?share=" + url.QueryEscape(token)
	previewURL := origin + "/m/" + url.PathEscape(meme.ID) + "/preview/" + url.PathEscape(sharedAssetFileName(meme, true)) + "?share=" + url.QueryEscape(token)
	openURL := "/m/" + url.PathEscape(meme.ID)
	title := "Shared meme from MemeIndex"
	mediaTag := `<a class="file" href="` + html.EscapeString(mediaURL) + `">Open shared file</a>`
	meta := `<meta property="og:image" content="` + html.EscapeString(origin+"/og-image.svg") + `"><meta name="twitter:card" content="summary_large_image">`
	switch {
	case strings.HasPrefix(meme.ContentType, "image/"):
		meta = `<meta property="og:image" content="` + html.EscapeString(mediaURL) + `"><meta property="og:image:url" content="` + html.EscapeString(mediaURL) + `"><meta property="og:image:secure_url" content="` + html.EscapeString(mediaURL) + `"><meta property="og:image:type" content="` + html.EscapeString(meme.ContentType) + `"><meta property="og:image:alt" content="Shared meme"><meta name="twitter:card" content="summary_large_image"><meta name="twitter:image" content="` + html.EscapeString(mediaURL) + `"><meta name="twitter:image:alt" content="Shared meme">`
		mediaTag = `<img src="` + html.EscapeString(mediaURL) + `" alt="Shared meme">`
	case strings.HasPrefix(meme.ContentType, "video/"):
		meta = `<meta property="og:video" content="` + html.EscapeString(mediaURL) + `"><meta property="og:video:secure_url" content="` + html.EscapeString(mediaURL) + `"><meta property="og:video:type" content="` + html.EscapeString(meme.ContentType) + `">`
		if meme.PreviewPath != "" {
			meta += `<meta property="og:image" content="` + html.EscapeString(previewURL) + `"><meta property="og:image:secure_url" content="` + html.EscapeString(previewURL) + `"><meta property="og:image:type" content="image/jpeg"><meta property="og:image:alt" content="Shared meme preview"><meta name="twitter:card" content="summary_large_image"><meta name="twitter:image" content="` + html.EscapeString(previewURL) + `">`
		}
		mediaTag = `<video src="` + html.EscapeString(mediaURL) + `" controls autoplay loop playsinline></video>`
	case strings.HasPrefix(meme.ContentType, "audio/"):
		meta = `<meta property="og:audio" content="` + html.EscapeString(mediaURL) + `"><meta property="og:audio:secure_url" content="` + html.EscapeString(mediaURL) + `"><meta property="og:audio:type" content="` + html.EscapeString(meme.ContentType) + `">`
		mediaTag = `<audio src="` + html.EscapeString(mediaURL) + `" controls autoplay></audio>`
	}

	page := `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>` + title + `</title><meta name="robots" content="noindex,nofollow,noarchive"><meta property="og:type" content="website"><meta property="og:site_name" content="MemeIndex"><meta property="og:title" content="` + title + `"><meta property="og:description" content="Shared privately for 30 days."><meta property="og:url" content="` + html.EscapeString(shareURL) + `"><meta name="twitter:title" content="` + title + `"><meta name="twitter:description" content="Shared privately for 30 days.">` + meta + `<style>html,body{margin:0;min-height:100%;background:#070b0a;color:#eef7f0;font:16px system-ui}body{display:grid;place-items:center;padding:24px;box-sizing:border-box}.wrap{width:min(1100px,100%);text-align:center}img,video{max-width:100%;max-height:82vh;border-radius:12px;background:#000}audio{width:min(680px,100%)}a{color:#72c77a}.open{display:inline-block;margin-top:18px;padding:11px 16px;border:1px solid #315d3b;border-radius:9px;text-decoration:none}.meta{color:#91a397;font-size:13px}</style></head><body><main class="wrap">` + mediaTag + `<p><a class="open" href="` + html.EscapeString(openURL) + `">Open in MemeIndex</a></p><p class="meta">Public link expires ` + html.EscapeString(expiresAt.Local().Format("January 2, 2006")) + `.</p></main></body></html>`
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self'; media-src 'self'; style-src 'unsafe-inline'; base-uri 'none'; frame-ancestors 'none'")
	_, _ = w.Write([]byte(page))
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
	host := forwardedHeaderFirstValue(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = r.Host
	}
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
	if err != nil || parsed.IsAbs() || parsed.Host != "" {
		return ""
	}
	return parsed.RequestURI()
}
