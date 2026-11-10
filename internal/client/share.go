package client

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
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
	"memeindex/internal/manager"
)

const memeShareLifetime = manager.MemeShareLifetime

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
	// The link is the file itself, so one URL serves every purpose; the
	// separate keys remain for clients written against the older response.
	link := s.memeShareURL(r, share, meme)
	writeJSON(w, http.StatusCreated, map[string]any{
		"url":        link,
		"page_url":   link,
		"media_url":  link,
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
			// A link from before share links pointed straight at the file.
			// It now behaves like the newer /s/ links: every client gets the
			// file itself, with no page or User-Agent sniffing in between.
			s.serveSharedMemeAsset(w, r, meme, false)
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

// handleShareLink serves /s/<code>.<ext>, the public link to a shared meme's
// file. The extension is only there so chat apps recognise the media type; the
// code alone identifies the share. A code whose share has expired, been
// revoked, or been replaced by a newer share redirects to the meme's
// sign-in-protected page instead of serving the file.
func (s *Server) handleShareLink(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	w.Header().Set("CDN-Cache-Control", "no-store")
	w.Header().Set("Cloudflare-CDN-Cache-Control", "no-store")
	w.Header().Set("Surrogate-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/s/")
	if strings.Contains(name, "/") {
		http.NotFound(w, r)
		return
	}
	slug, _, _ := strings.Cut(name, ".")
	if len(slug) != shareSlugLength {
		http.NotFound(w, r)
		return
	}
	share, meme, current, err := s.managers.ResolveMemeShareLink(slug)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			http.Error(w, "failed to load shared meme", http.StatusInternalServerError)
			return
		}
		http.NotFound(w, r)
		return
	}
	if current && share.ExpiresAt.After(time.Now().UTC()) {
		s.serveSharedMemeAsset(w, r, meme, false)
		return
	}
	http.Redirect(w, r, "/m/"+url.PathEscape(meme.ID), http.StatusFound)
}

// shareSlugLength is the length of a share link code, a random UUID.
const shareSlugLength = 36

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
				"url":                    s.memeShareURL(r, entry.Share, entry.Meme),
				"page_url":               s.memeShareURL(r, entry.Share, entry.Meme),
				"media_url":              s.memeShareURL(r, entry.Share, entry.Meme),
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

// memeShareURL returns the public link to a shared meme's file.
func (s *Server) memeShareURL(r *http.Request, share accessor.MemeShareState, meme accessor.Meme) string {
	if share.Slug == "" {
		// A share created before links had codes keeps its signed link until
		// it is next renewed, which issues it a code.
		return s.publicOrigin(r) + "/m/" + url.PathEscape(share.MemeID) + "?share=" + url.QueryEscape(s.signMemeShare(share))
	}
	return s.publicOrigin(r) + "/s/" + url.PathEscape(share.Slug) + filepath.Ext(sharedAssetFileName(meme, false))
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
	return s.signMemeShareValues(share.MemeID, share.ExpiresAt.UTC().Unix(), share.Generation)
}

func (s *Server) signMemeShareValues(memeID string, expiresUnix, generationValue int64) string {
	expires := strconv.FormatInt(expiresUnix, 10)
	generation := strconv.FormatInt(generationValue, 10)
	payload := memeID + "\n" + expires + "\n" + generation
	mac := hmac.New(sha256.New, s.shareSecret)
	_, _ = mac.Write([]byte(payload))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return expires + "." + generation + "." + signature
}

// validMemeShareToken checks a signed link from before share links had codes.
// The token carries its own expiry, which the signature covers; it is honoured
// until then as long as the share it was issued for is still the meme's
// current, unexpired one. Renewing a share moves the share's expiry without
// changing its generation, so the token's expiry is deliberately not required
// to equal the share's.
func (s *Server) validMemeShareToken(token string, share accessor.MemeShareState, now time.Time) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || len(s.shareSecret) == 0 || !share.ExpiresAt.After(now) {
		return false
	}
	expires, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || now.Unix() >= expires {
		return false
	}
	generation, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || generation != share.Generation {
		return false
	}
	expected := s.signMemeShareValues(share.MemeID, expires, generation)
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
