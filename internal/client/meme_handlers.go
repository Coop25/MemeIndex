package client

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"

	"memeindex/internal/accessor"
	"memeindex/internal/tagsuggest"
)

func (s *Server) handleMemes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listMemes(w, r)
	case http.MethodPost:
		if !s.requirePermission(w, r, permissionUpload) {
			return
		}
		s.createMeme(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleMemeByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/memes/")
	if path == "" {
		http.NotFound(w, r)
		return
	}

	if strings.HasSuffix(path, "/favorite") {
		id := strings.TrimSuffix(path, "/favorite")
		id = strings.TrimSuffix(id, "/")
		if id == "" || strings.Contains(id, "/") {
			http.NotFound(w, r)
			return
		}

		if r.Method != http.MethodPatch {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.requirePermission(w, r, permissionView) {
			return
		}
		s.updateFavorite(w, r, id)
		return
	}

	if strings.HasSuffix(path, "/share") {
		id := strings.TrimSuffix(path, "/share")
		id = strings.TrimSuffix(id, "/")
		if id == "" || strings.Contains(id, "/") {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.createMemeShare(w, r, id)
		return
	}

	if strings.HasSuffix(path, "/tag-suggestions") {
		id := strings.TrimSuffix(path, "/tag-suggestions")
		id = strings.TrimSuffix(id, "/")
		if id == "" || strings.Contains(id, "/") {
			http.NotFound(w, r)
			return
		}

		if !s.requirePermission(w, r, permissionMetadata) {
			return
		}

		switch r.Method {
		case http.MethodGet:
			s.getStoredMemeTagSuggestions(w, r, id)
		case http.MethodPatch:
			s.updateStoredMemeTagSuggestions(w, r, id)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	if strings.HasSuffix(path, "/tag-suggestions/refresh") {
		id := strings.TrimSuffix(path, "/tag-suggestions/refresh")
		id = strings.TrimSuffix(id, "/")
		if id == "" || strings.Contains(id, "/") {
			http.NotFound(w, r)
			return
		}

		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.requirePermission(w, r, permissionMetadata) {
			return
		}
		s.refreshMemeTagSuggestions(w, r, id)
		return
	}

	id := path
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodGet:
		meme, err := s.managers.GetMeme(currentUserID(r), id)
		if errors.Is(err, os.ErrNotExist) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "failed to load meme", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, s.protectMemeForResponse(r, meme))
	case http.MethodPatch:
		s.updateMeme(w, r, id)
	case http.MethodDelete:
		if !s.requirePermission(w, r, permissionDeleteMemes) {
			return
		}
		s.deleteMeme(w, r, id)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) getStoredMemeTagSuggestions(w http.ResponseWriter, r *http.Request, id string) {
	result, err := s.managers.GetStoredMemeTagSuggestions(id)
	if err == nil {
		writeJSON(w, http.StatusOK, result)
		return
	}

	switch {
	case errors.Is(err, os.ErrNotExist):
		http.NotFound(w, r)
	case errors.Is(err, tagsuggest.ErrDisabled):
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": "tag suggestions are disabled",
		})
	case errors.Is(err, tagsuggest.ErrUnsupported):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error": "tag suggestions currently work for images and videos with generated thumbnails",
		})
	case errors.Is(err, tagsuggest.ErrUnavailable):
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": err.Error(),
		})
	default:
		log.Printf("suggest meme tags failed: %v", err)
		http.Error(w, "failed to suggest tags", http.StatusInternalServerError)
	}
}

func (s *Server) refreshMemeTagSuggestions(w http.ResponseWriter, r *http.Request, id string) {
	result, err := s.managers.RefreshMemeTagSuggestions(r.Context(), currentUserID(r), id)
	if err == nil {
		writeJSON(w, http.StatusOK, result)
		return
	}

	switch {
	case errors.Is(err, os.ErrNotExist):
		http.NotFound(w, r)
	case errors.Is(err, tagsuggest.ErrDisabled):
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": "tag suggestions are disabled",
		})
	case errors.Is(err, tagsuggest.ErrUnsupported):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error": "tag suggestions currently work for images and videos with generated thumbnails",
		})
	case errors.Is(err, tagsuggest.ErrUnavailable):
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": err.Error(),
		})
	default:
		log.Printf("refresh meme tag suggestions failed: %v", err)
		http.Error(w, "failed to suggest tags", http.StatusInternalServerError)
	}
}

func (s *Server) updateStoredMemeTagSuggestions(w http.ResponseWriter, r *http.Request, id string) {
	var payload struct {
		Action string `json:"action"`
		Tag    string `json:"tag"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	switch strings.ToLower(strings.TrimSpace(payload.Action)) {
	case "dismiss":
		meme, err := s.managers.DismissMemeTagSuggestion(currentUserID(r), id, payload.Tag)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				http.NotFound(w, r)
				return
			}
			log.Printf("dismiss meme tag suggestion failed: %v", err)
			http.Error(w, "failed to dismiss tag suggestion", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, s.protectMemeForResponse(r, meme))
	case "dismiss_all":
		meme, err := s.managers.DismissAllMemeTagSuggestions(currentUserID(r), id)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				http.NotFound(w, r)
				return
			}
			log.Printf("dismiss all meme tag suggestions failed: %v", err)
			http.Error(w, "failed to dismiss tag suggestions", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, s.protectMemeForResponse(r, meme))
	case "add":
		if s.auth.enabled() {
			session, ok := sessionFromContext(r.Context())
			if !ok || (!session.Permissions.CanAddTags && !session.Permissions.CanManageUsers) {
				writeJSON(w, http.StatusForbidden, map[string]any{"error": "missing add-tag permission"})
				return
			}
		}
		meme, err := s.managers.ApplyMemeTagSuggestion(currentUserID(r), id, payload.Tag, currentAuditActor(r))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				http.NotFound(w, r)
				return
			}
			log.Printf("apply meme tag suggestion failed: %v", err)
			http.Error(w, "failed to apply tag suggestion", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, s.protectMemeForResponse(r, meme))
	default:
		http.Error(w, "invalid action", http.StatusBadRequest)
	}
}

func (s *Server) listMemes(w http.ResponseWriter, r *http.Request) {
	favoritesOnly := r.URL.Query().Get("favorites") == "true"
	tag := r.URL.Query().Get("tag")
	query := r.URL.Query().Get("q")
	view := r.URL.Query().Get("view")
	sortBy := r.URL.Query().Get("sort")
	userID := currentUserID(r)
	offset := parseQueryInt(r, "offset", 0)
	limit := parseQueryInt(r, "limit", 72)

	writeJSON(w, http.StatusOK, s.protectMemeListForResponse(r, s.managers.ListMemesSorted(userID, query, favoritesOnly, tag, view, sortBy, offset, limit)))
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// ?probe=newest answers "has anything new arrived?" for the home screen's
	// background poll with a single id, instead of rebuilding and shipping the
	// whole dashboard every few seconds per open tab.
	if r.URL.Query().Get("probe") == "newest" {
		writeJSON(w, http.StatusOK, map[string]any{"newest_id": s.managers.NewestMemeID(currentUserID(r))})
		return
	}
	dashboard := s.managers.Dashboard(currentUserID(r))
	dashboard.RecentItems = s.protectMemesForResponse(r, dashboard.RecentItems)
	dashboard.FavoriteItems = s.protectMemesForResponse(r, dashboard.FavoriteItems)
	dashboard.RandomItems = s.protectMemesForResponse(r, dashboard.RandomItems)
	writeJSON(w, http.StatusOK, dashboard)
}

func (s *Server) createMeme(w http.ResponseWriter, r *http.Request) {
	if s.enforceRateLimit(w, r, s.writeLimiter, "upload") {
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
			http.Error(w, "upload exceeds size limit", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "invalid multipart request", http.StatusBadRequest)
		return
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()

	sourceURL := strings.TrimSpace(r.FormValue("source_url"))
	fileHeaders := r.MultipartForm.File["file"]
	if sourceURL != "" && len(fileHeaders) > 0 {
		http.Error(w, "choose either files or a link", http.StatusBadRequest)
		return
	}
	if sourceURL == "" && len(fileHeaders) == 0 {
		http.Error(w, "file or source link is required", http.StatusBadRequest)
		return
	}

	if sourceURL != "" {
		if s.enforceRateLimit(w, r, s.linkImportLimiter, "linkimport") {
			return
		}
		meme, err, pending := s.importLinkWithinBudget(r, currentAuditActor(r), sourceURL, splitTags(r.FormValue("tags")), r.FormValue("notes"))
		if pending {
			writeJSON(w, http.StatusAccepted, map[string]any{
				"pending": true,
				"queued":  false,
			})
			return
		}
		if err != nil {
			var duplicateErr *accessor.DuplicateMemeError
			if errors.As(err, &duplicateErr) {
				writeJSON(w, http.StatusOK, map[string]any{
					"memes":   []accessor.Meme{},
					"created": 0,
					"duplicates": []map[string]any{{
						"filename":       duplicateErr.Existing.OriginalName,
						"existing":       s.protectMemeForResponse(r, duplicateErr.Existing),
						"pending_delete": duplicateErr.PendingDelete,
					}},
					"skipped": 1,
				})
				return
			}

			status := http.StatusBadGateway
			switch {
			case errors.Is(err, errUnsupportedMediaURL):
				status = http.StatusBadRequest
			case strings.Contains(strings.ToLower(err.Error()), "not configured"):
				status = http.StatusServiceUnavailable
			case s.shouldQueueLinkRetry(sourceURL, err):
				job := s.linkRetries.Enqueue(sourceURL, splitTags(r.FormValue("tags")), r.FormValue("notes"), currentAuditActor(r))
				log.Printf("link import of %q failed, queued for retry: %v", sanitizeLogField(sourceURL), err)
				writeJSON(w, http.StatusAccepted, map[string]any{
					"queued": true,
					"job":    job,
					"error":  clientSafeError(err, "the download failed for now"),
				})
				return
			}
			log.Printf("link import of %q failed: %v", sanitizeLogField(sourceURL), err)
			http.Error(w, clientSafeError(err, "could not download that link"), status)
			return
		}

		writeJSON(w, http.StatusCreated, map[string]any{
			"memes":      s.protectMemesForResponse(r, []accessor.Meme{meme}),
			"created":    1,
			"duplicates": []map[string]any{},
			"skipped":    0,
		})
		s.managers.QueueMemeTagSuggestions(meme.ID)
		return
	}

	created := make([]accessor.Meme, 0, len(fileHeaders))
	duplicates := make([]map[string]any, 0)
	// A file that cannot be saved is reported alongside the ones that were,
	// instead of failing the whole request: earlier files in the batch are
	// already stored, and answering 500 for them made the client retry the
	// batch into a wall of duplicates.
	failed := make([]map[string]any, 0)
	for _, fileHeader := range fileHeaders {
		src, err := fileHeader.Open()
		if err != nil {
			log.Printf("open upload file failed: %v", err)
			failed = append(failed, map[string]any{"filename": fileHeader.Filename, "error": "failed to read upload"})
			continue
		}

		meme, err := s.managers.CreateMemeAs(
			currentAuditActor(r),
			src,
			fileHeader.Header,
			fileHeader.Filename,
			splitTags(r.FormValue("tags")),
			r.FormValue("notes"),
		)
		_ = src.Close()
		if err != nil {
			var duplicateErr *accessor.DuplicateMemeError
			if errors.As(err, &duplicateErr) {
				duplicates = append(duplicates, map[string]any{
					"filename":       fileHeader.Filename,
					"existing":       s.protectMemeForResponse(r, duplicateErr.Existing),
					"pending_delete": duplicateErr.PendingDelete,
				})
				continue
			}
			log.Printf("create meme failed: %v", err)
			failed = append(failed, map[string]any{"filename": fileHeader.Filename, "error": "failed to save meme"})
			continue
		}

		created = append(created, meme)
		s.managers.QueueMemeTagSuggestions(meme.ID)
	}

	if len(created) == 0 && len(duplicates) == 0 {
		http.Error(w, "failed to save meme", http.StatusInternalServerError)
		return
	}
	status := http.StatusCreated
	if len(created) == 0 {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]any{
		"memes":      s.protectMemesForResponse(r, created),
		"created":    len(created),
		"duplicates": duplicates,
		"skipped":    len(duplicates),
		"failed":     failed,
	})
}

// memePatchPayload is the PATCH /api/memes/{id} body. Every field is
// optional and an omitted one is left as it is - so a client that only edits
// notes cannot silently wipe tags or unfavorite the meme. Tags can be sent
// either as the full replacement set ("tags") or, preferably, as a delta
// against what the client last saw ("tags_added"/"tags_removed"), which is
// applied to the meme's current tags so two people tagging the same meme at
// once don't overwrite each other's work.
type memePatchPayload struct {
	Tags        *[]string `json:"tags"`
	TagsAdded   []string  `json:"tags_added"`
	TagsRemoved []string  `json:"tags_removed"`
	Notes       *string   `json:"notes"`
	Favorite    *bool     `json:"favorite"`
}

// resolveMemePatch turns a partial PATCH body into the full update the store
// applies, starting from the meme's current state.
func resolveMemePatch(existing accessor.Meme, payload memePatchPayload) accessor.MemeUpdate {
	update := accessor.MemeUpdate{
		Tags:     append([]string(nil), existing.Tags...),
		Notes:    existing.Notes,
		Favorite: existing.Favorite,
	}
	switch {
	case payload.TagsAdded != nil || payload.TagsRemoved != nil:
		removed := map[string]struct{}{}
		for _, tag := range normalizePermissionTags(payload.TagsRemoved) {
			removed[tag] = struct{}{}
		}
		next := make([]string, 0, len(existing.Tags)+len(payload.TagsAdded))
		for _, tag := range normalizePermissionTags(existing.Tags) {
			if _, drop := removed[tag]; !drop {
				next = append(next, tag)
			}
		}
		update.Tags = normalizePermissionTags(append(next, payload.TagsAdded...))
	case payload.Tags != nil:
		update.Tags = *payload.Tags
	}
	if payload.Notes != nil {
		update.Notes = *payload.Notes
	}
	if payload.Favorite != nil {
		update.Favorite = *payload.Favorite
	}
	return update
}

func (s *Server) updateMeme(w http.ResponseWriter, r *http.Request, id string) {
	var patch memePatchPayload
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	// With authentication disabled (MEMEINDEX_ALLOW_ANONYMOUS) there is no
	// session at all, and every caller may do everything - the same policy
	// handleAuthSession advertises to the frontend.
	session, ok := sessionFromContext(r.Context())
	if !s.auth.enabled() {
		session, ok = authSession{Permissions: authPermissions{CanView: true, CanAddTags: true, CanRemoveTags: true, CanManageUsers: true}}, true
	}
	if !ok {
		writeJSON(w, http.StatusForbidden, map[string]any{
			"error": "insufficient permissions",
		})
		return
	}

	existing, err := s.managers.GetMeme(currentUserID(r), id)
	if errors.Is(err, os.ErrNotExist) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		log.Printf("load meme for update failed: %v", err)
		http.Error(w, "failed to load meme", http.StatusInternalServerError)
		return
	}

	payload := resolveMemePatch(existing, patch)
	currentTags := normalizePermissionTags(existing.Tags)
	nextTags := normalizePermissionTags(payload.Tags)
	addedTags, removedTags := diffTags(currentTags, nextTags)
	payload.Actor = currentAuditActor(r)

	if len(addedTags) > 0 && !session.Permissions.CanAddTags && !session.Permissions.CanManageUsers {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "missing add-tag permission"})
		return
	}
	if len(removedTags) > 0 && !session.Permissions.CanRemoveTags && !session.Permissions.CanManageUsers {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "missing remove-tag permission"})
		return
	}
	if strings.TrimSpace(payload.Notes) != strings.TrimSpace(existing.Notes) &&
		!session.Permissions.CanAddTags && !session.Permissions.CanRemoveTags && !session.Permissions.CanManageUsers {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "missing metadata permission"})
		return
	}
	if payload.Favorite != existing.Favorite && !session.Permissions.CanView {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "missing favorite permission"})
		return
	}

	meme, err := s.managers.UpdateMeme(currentUserID(r), id, payload)
	if err != nil {
		log.Printf("update meme failed: %v", err)
		http.Error(w, "failed to update meme", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, s.protectMemeForResponse(r, meme))
}

func (s *Server) updateFavorite(w http.ResponseWriter, r *http.Request, id string) {
	var payload struct {
		Favorite bool `json:"favorite"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	meme, err := s.managers.SetFavoriteAs(currentUserID(r), id, payload.Favorite, currentAuditActor(r))
	if errors.Is(err, os.ErrNotExist) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		log.Printf("update favorite failed: %v", err)
		http.Error(w, "failed to update favorite", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, s.protectMemeForResponse(r, meme))
}

func (s *Server) deleteMeme(w http.ResponseWriter, r *http.Request, id string) {
	result, err := s.managers.DeleteMeme(id, currentAuditActor(r))
	if errors.Is(err, os.ErrNotExist) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		log.Printf("delete meme failed: %v", err)
		http.Error(w, "failed to delete meme", http.StatusInternalServerError)
		return
	}

	if result.PendingApproval {
		writeJSON(w, http.StatusAccepted, map[string]any{
			"pending_approval": true,
		})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleTags(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	query := r.URL.Query().Get("q")
	writeJSON(w, http.StatusOK, map[string]any{
		"tags": s.managers.SuggestTags(query, 8),
	})
}

func (s *Server) handlePopularTags(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	limit := parseQueryInt(r, "limit", 10)
	writeJSON(w, http.StatusOK, map[string]any{
		"tags": s.managers.PopularTags(currentUserID(r), limit),
	})
}

func (s *Server) handleRandomMeme(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID, validSession := s.reelID(r, r.URL.Query().Get("session_id"))
	if !validSession {
		sessionID = ""
	}
	direction := r.URL.Query().Get("direction")
	result, err := s.managers.StepRandomReel(sessionID, direction)
	if errors.Is(err, os.ErrNotExist) {
		http.Error(w, "no memes available", http.StatusNotFound)
		return
	}
	if err != nil {
		log.Printf("random meme failed: %v", err)
		http.Error(w, "failed to pick random meme", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"session_id":       s.reelToken(r, result.SessionID),
		"session_replaced": result.SessionReplaced || !validSession,
		"reason":           result.Reason,
		"can_go_prev":      result.CanGoPrev,
		"meme":             s.protectMemeForResponse(r, result.Meme),
		"prev_memes":       s.protectMemesForResponse(r, result.PrevMemes),
		"next_memes":       s.protectMemesForResponse(r, result.NextMemes),
	})
}

func (s *Server) handleReelSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID, validSession := s.reelID(r, r.URL.Query().Get("session_id"))
	if !validSession {
		http.NotFound(w, r)
		return
	}
	if err := s.managers.DeleteRandomReelSession(sessionID); err != nil {
		log.Printf("delete reel session failed: %v", err)
		http.Error(w, "failed to delete reel session", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
