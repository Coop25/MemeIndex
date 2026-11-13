package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"mime"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	mediafetch "github.com/Coop25/mediafetch-go"
	"memeindex/internal/accessor"
)

func (s *Server) handleSaveLink(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.enforceRateLimit(w, r, s.linkImportLimiter, "linkimport") {
		return
	}
	var payload struct {
		URL      string   `json:"url"`
		Title    string   `json:"title"`
		Notes    string   `json:"notes"`
		Tags     []string `json:"tags"`
		Favorite bool     `json:"favorite"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	if err := decoder.Decode(&payload); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	parsed, err := url.Parse(strings.TrimSpace(payload.URL))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil {
		http.Error(w, "a public HTTP or HTTPS URL is required", http.StatusBadRequest)
		return
	}
	title := strings.TrimSpace(payload.Title)
	if title == "" {
		title = parsed.Hostname()
	}
	filename := safeLinkFilename(title) + ".url"
	body := "[InternetShortcut]\r\nURL=" + parsed.String() + "\r\n"
	header := textproto.MIMEHeader{}
	header.Set("Content-Type", "application/internet-shortcut")
	meme, err := s.managers.CreateMemeAsWithSource(currentAuditActor(r), strings.NewReader(body), header, filename, payload.Tags, payload.Notes, parsed.String())
	if err != nil {
		var duplicateErr *accessor.DuplicateMemeError
		if errors.As(err, &duplicateErr) {
			message := "this link is already saved"
			if duplicateErr.PendingDelete {
				message = "this link is already saved, but is waiting for a delete request to be reviewed"
			}
			writeJSON(w, http.StatusConflict, map[string]any{"error": message, "existing": s.protectMemeForResponse(r, duplicateErr.Existing)})
			return
		}
		log.Printf("save external link failed: %v", err)
		http.Error(w, "failed to save link", http.StatusInternalServerError)
		return
	}
	if payload.Favorite {
		if updated, favoriteErr := s.managers.SetFavorite(currentUserID(r), meme.ID, true); favoriteErr == nil {
			meme = updated
		}
	}
	writeJSON(w, http.StatusCreated, s.protectMemeForResponse(r, meme))
}

func safeLinkFilename(value string) string {
	value = strings.TrimSpace(value)
	var output strings.Builder
	for _, char := range value {
		if char == '/' || char == '\\' || char == ':' || char == '*' || char == '?' || char == '"' || char == '<' || char == '>' || char == '|' || char < 32 {
			output.WriteRune('-')
			continue
		}
		output.WriteRune(char)
		if output.Len() >= 120 {
			break
		}
	}
	clean := strings.Trim(strings.TrimSpace(output.String()), ".")
	if clean == "" {
		return "saved-link"
	}
	return clean
}

var errUnsupportedMediaURL = userError("use a direct image or video link (including Discord attachments), or a supported YouTube, Facebook, Reddit, Instagram, TikTok, or Twitter/X link")

var errUndownloadableSourceURL = userError("this link looks valid, but the source video is not directly downloadable from that URL. Try opening the post/video itself and copying the direct Facebook video URL instead of the share link")

func (s *Server) createMemeFromSourceURL(ctx context.Context, actor accessor.AuditActor, sourceURL string, tags []string, notes string) (accessor.Meme, error) {
	sourceURL = strings.TrimSpace(sourceURL)
	if isDirectMediaURL(sourceURL) {
		return s.createMemeFromDirectURL(ctx, actor, sourceURL, tags, notes)
	}
	if s.mediaClient == nil {
		return accessor.Meme{}, userError("link downloads are not configured")
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	resolvedSourceURL, err := normalizeSourceURL(ctx, sourceURL)
	if err != nil {
		log.Printf("normalize source url failed for %q: %v", sourceURL, err)
		return accessor.Meme{}, err
	}
	if isDirectMediaURL(resolvedSourceURL) {
		return s.createMemeFromDirectURL(ctx, actor, sourceURL, tags, notes)
	}

	candidates := sourceURLCandidates(sourceURL, resolvedSourceURL)
	supportedCandidates := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if supportedSourceURL(candidate) {
			supportedCandidates = append(supportedCandidates, candidate)
		}
	}
	if len(supportedCandidates) == 0 {
		return accessor.Meme{}, errUnsupportedMediaURL
	}

	var (
		downloadedFile        string
		successfulDownload    string
		successfulDownloadDir string
		lastDownloadErr       error
	)
	for _, candidate := range supportedCandidates {
		downloadID, _, extractErr := s.mediaClient.Extract(ctx, candidate)
		if extractErr != nil {
			log.Printf("media extract failed for original=%q resolved=%q candidate=%q: %v", sourceURL, resolvedSourceURL, candidate, extractErr)
			lastDownloadErr = extractErr
			continue
		}

		downloadDir := filepath.Join(s.mediaClient.DownloadDir(), downloadID)
		file, downloadErr := s.mediaClient.Download(ctx, candidate, downloadID, "best")
		if downloadErr != nil {
			log.Printf("media download failed for original=%q resolved=%q candidate=%q download_id=%q: %v", sourceURL, resolvedSourceURL, candidate, downloadID, downloadErr)
			_ = os.RemoveAll(downloadDir)
			lastDownloadErr = downloadErr
			continue
		}

		downloadedFile = file
		successfulDownload = candidate
		successfulDownloadDir = downloadDir
		break
	}
	if downloadedFile == "" {
		return accessor.Meme{}, friendlySourceDownloadError(sourceURL, resolvedSourceURL, lastDownloadErr)
	}
	defer func() { _ = os.RemoveAll(successfulDownloadDir) }()

	src, err := os.Open(downloadedFile)
	if err != nil {
		log.Printf("open downloaded media failed for original=%q resolved=%q selected=%q file=%q: %v", sourceURL, resolvedSourceURL, successfulDownload, downloadedFile, err)
		return accessor.Meme{}, err
	}
	defer func() { _ = src.Close() }()

	filename := filepath.Base(downloadedFile)
	header := textproto.MIMEHeader{}
	if contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(filename))); contentType != "" {
		header.Set("Content-Type", contentType)
	}

	return s.managers.CreateMemeAsWithSource(
		actor,
		src,
		header,
		filename,
		tags,
		notes,
		sourceURL,
	)
}

func (s *Server) shouldQueueLinkRetry(sourceURL string, err error) bool {
	if s.linkRetries == nil || strings.TrimSpace(sourceURL) == "" || err == nil {
		return false
	}
	var duplicateErr *accessor.DuplicateMemeError
	if errors.As(err, &duplicateErr) || errors.Is(err, errUnsupportedMediaURL) {
		return false
	}
	if strings.Contains(strings.ToLower(err.Error()), "not configured") {
		return false
	}
	return isDirectMediaURL(sourceURL) || supportedSourceURL(sourceURL)
}

func (s *Server) processRetriedLinkJob(ctx context.Context, job LinkRetryJob) error {
	if s.auth.enabled() && !hasPermission(s.auth.permissionsForUser(job.Actor.UserID), permissionUpload) {
		return errors.New("upload permission was revoked before retry")
	}
	meme, err := s.createMemeFromSourceURL(ctx, job.Actor, job.SourceURL, job.Tags, job.Notes)
	if err != nil {
		var duplicateErr *accessor.DuplicateMemeError
		if errors.As(err, &duplicateErr) {
			return nil
		}
		return err
	}
	s.managers.QueueMemeTagSuggestions(meme.ID)
	return nil
}

func friendlySourceDownloadError(originalURL, resolvedURL string, err error) error {
	if err == nil {
		return errUndownloadableSourceURL
	}

	message := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case strings.Contains(message, "unsupported url"):
		if mediafetch.IsFacebookURL(originalURL) || mediafetch.IsFacebookURL(resolvedURL) {
			return errUndownloadableSourceURL
		}
	case strings.Contains(message, "private video"),
		strings.Contains(message, "requires login"),
		strings.Contains(message, "not directly downloadable"):
		return userError("this video is not publicly downloadable from that link")
	}

	return err
}

func normalizeSourceURL(ctx context.Context, raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if !supportedSourceURL(trimmed) {
		return "", errUnsupportedMediaURL
	}
	if err := validateRemoteURL(ctx, trimmed); err != nil {
		return "", err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, trimmed, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("User-Agent", "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1")

	transport := newPublicRemoteTransport()
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport: transport,
		Timeout:   20 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			if !supportedSourceURL(req.URL.String()) && !isDirectMediaURL(req.URL.String()) {
				return errUnsupportedMediaURL
			}
			return validateRemoteURL(req.Context(), req.URL.String())
		},
	}

	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer func() { _ = response.Body.Close() }()

	if response.Request == nil || response.Request.URL == nil {
		return trimmed, nil
	}

	resolved := strings.TrimSpace(response.Request.URL.String())
	if resolved == "" {
		return trimmed, nil
	}

	if err := validateRemoteURL(ctx, resolved); err != nil {
		return "", err
	}
	return resolved, nil
}

func validateRemoteURL(ctx context.Context, raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Hostname() == "" || parsed.User != nil {
		return userError("invalid remote URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return userError("only HTTP and HTTPS remote URLs are allowed")
	}
	host := parsed.Hostname()
	addresses := []net.IP{}
	if literal := net.ParseIP(host); literal != nil {
		addresses = append(addresses, literal)
	} else {
		resolved, lookupErr := net.DefaultResolver.LookupIPAddr(ctx, host)
		if lookupErr != nil {
			return fmt.Errorf("resolve remote host: %w", lookupErr)
		}
		for _, address := range resolved {
			addresses = append(addresses, address.IP)
		}
	}
	if len(addresses) == 0 {
		return userError("remote host did not resolve")
	}
	for _, address := range addresses {
		if !publicRemoteIP(address) {
			return userError("private and local network addresses are not allowed")
		}
	}
	return nil
}

func sourceURLCandidates(original string, resolved string) []string {
	candidates := make([]string, 0, 2)
	seen := make(map[string]struct{}, 2)

	for _, candidate := range []string{strings.TrimSpace(resolved), strings.TrimSpace(original)} {
		if candidate == "" {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		candidates = append(candidates, candidate)
	}

	return candidates
}
