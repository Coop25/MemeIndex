package client

import (
	"errors"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"memeindex/internal/accessor"
)

// handleShareTarget receives content sent to the installed PWA through the
// operating system share sheet. The web app manifest points its share_target
// action here, so a share becomes a top-level multipart POST navigation that
// carries either attached files or a shared link/text. Files reuse the normal
// upload pipeline and a shared URL is routed through the same link-import path
// as the Process Link modal. The handler always redirects back to the app shell
// with a ?shared= status the frontend turns into a toast.
func (s *Server) handleShareTarget(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Sharing into the archive is an upload, so it needs the same upload
	// permission and the same rate limits as POST /api/memes - otherwise a
	// view-only account could upload (and start unlimited link downloads)
	// simply by going through the share sheet.
	if s.auth.enabled() {
		session, ok := sessionFromContext(r.Context())
		if !ok || !hasPermission(session.Permissions, permissionUpload) {
			http.Redirect(w, r, "/?shared=forbidden", http.StatusSeeOther)
			return
		}
	}
	if s.writeLimiter != nil && !s.writeLimiter.allow("upload|"+s.rateLimitIdentity(r)) {
		http.Redirect(w, r, "/?shared=ratelimited", http.StatusSeeOther)
		return
	}

	limit := s.config.MaxUploadBytes
	if limit <= 0 {
		limit = 256 << 20
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Redirect(w, r, "/?shared=toolarge", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/?shared=error", http.StatusSeeOther)
		return
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()

	actor := currentAuditActor(r)
	notes := sharedNotes(r.FormValue("title"), r.FormValue("text"))

	var fileHeaders []*multipart.FileHeader
	if r.MultipartForm != nil {
		fileHeaders = r.MultipartForm.File["files"]
	}

	if len(fileHeaders) > 0 {
		created, duplicates := 0, 0
		for _, fileHeader := range fileHeaders {
			src, err := fileHeader.Open()
			if err != nil {
				log.Printf("share-target: open shared file failed: %v", err)
				continue
			}
			meme, err := s.managers.CreateMemeAs(actor, src, fileHeader.Header, fileHeader.Filename, nil, notes)
			_ = src.Close()
			if err != nil {
				var duplicateErr *accessor.DuplicateMemeError
				if errors.As(err, &duplicateErr) {
					duplicates++
					continue
				}
				log.Printf("share-target: save shared file failed: %v", err)
				continue
			}
			created++
			s.managers.QueueMemeTagSuggestions(meme.ID)
		}
		switch {
		case created > 0:
			http.Redirect(w, r, "/?shared=ok&n="+strconv.Itoa(created), http.StatusSeeOther)
		case duplicates > 0:
			http.Redirect(w, r, "/?shared=dup", http.StatusSeeOther)
		default:
			http.Redirect(w, r, "/?shared=error", http.StatusSeeOther)
		}
		return
	}

	if sharedURL := firstSharedURL(r.FormValue("url"), r.FormValue("text")); sharedURL != "" {
		if s.linkImportLimiter != nil && !s.linkImportLimiter.allow("linkimport|"+s.rateLimitIdentity(r)) {
			http.Redirect(w, r, "/?shared=ratelimited", http.StatusSeeOther)
			return
		}
		meme, err, pending := s.importLinkWithinBudget(r, actor, sharedURL, nil, notes)
		if pending {
			http.Redirect(w, r, "/?shared=pending", http.StatusSeeOther)
			return
		}
		if err != nil {
			var duplicateErr *accessor.DuplicateMemeError
			switch {
			case errors.As(err, &duplicateErr):
				http.Redirect(w, r, "/?shared=dup", http.StatusSeeOther)
			case s.shouldQueueLinkRetry(sharedURL, err):
				s.linkRetries.Enqueue(sharedURL, nil, notes, actor)
				http.Redirect(w, r, "/?shared=queued", http.StatusSeeOther)
			default:
				log.Printf("share-target: link import failed for %q: %v", sharedURL, err)
				http.Redirect(w, r, "/?shared=linkerror", http.StatusSeeOther)
			}
			return
		}
		s.managers.QueueMemeTagSuggestions(meme.ID)
		http.Redirect(w, r, "/?shared=ok&n=1", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/?shared=empty", http.StatusSeeOther)
}

var sharedURLPattern = regexp.MustCompile(`https?://[^\s<>"']+`)

// firstSharedURL returns the first syntactically valid http(s) URL found across
// the supplied share fields. Apps often drop the link into the free-text "text"
// field instead of the dedicated "url" field, so both are scanned.
func firstSharedURL(values ...string) string {
	for _, value := range values {
		for _, candidate := range sharedURLPattern.FindAllString(value, -1) {
			candidate = strings.TrimRight(candidate, ".,);]")
			parsed, err := url.Parse(candidate)
			if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil {
				continue
			}
			return candidate
		}
	}
	return ""
}

// sharedNotes builds an optional note from the share sheet's title and text,
// keeping whichever fields are present, dropping a title the text already
// contains, and length-capping the result.
func sharedNotes(title, text string) string {
	title = strings.TrimSpace(title)
	text = strings.TrimSpace(text)
	switch {
	case title == "":
		return capSharedNote(text)
	case text == "":
		return capSharedNote(title)
	case strings.Contains(text, title):
		return capSharedNote(text)
	default:
		return capSharedNote(title + "\n" + text)
	}
}

func capSharedNote(note string) string {
	if len(note) > 2000 {
		// Back up to a character boundary so a multi-byte character straddling
		// the cap is dropped whole instead of being stored as invalid UTF-8.
		cut := 2000
		for cut > 0 && !utf8.RuneStart(note[cut]) {
			cut--
		}
		note = strings.TrimSpace(note[:cut])
	}
	return note
}
