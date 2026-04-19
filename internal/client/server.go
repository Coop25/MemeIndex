package client

import (
	"context"
	"encoding/json"
	"errors"
	"html"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"memeindex/internal/accessor"
	"memeindex/internal/manager"
)

type Server struct {
	config   Config
	managers *manager.MemeManager
	auth     *authService
	users    authUserStore
}

func NewServer(config Config, memeManager *manager.MemeManager) *Server {
	userStore, err := newAuthUserStore(context.Background(), config.DatabaseURL)
	if err != nil {
		log.Fatalf("auth user store init failed: %v", err)
	}
	if userStore != nil {
		if err := userStore.BootstrapFromConfig(context.Background(), config.DiscordAuth); err != nil {
			log.Fatalf("auth user bootstrap failed: %v", err)
		}
	}

	return &Server{
		config:   config,
		managers: memeManager,
		auth:     newAuthService(config.DiscordAuth, userStore),
		users:    userStore,
	}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/login", s.handleLogin)
	mux.HandleFunc("/auth/callback", s.handleOAuthCallback)
	mux.Handle("/forbidden", s.withPageAuth(http.HandlerFunc(s.handleAccessDeniedPage)))
	mux.HandleFunc("/auth/logout", s.handleLogout)
	mux.HandleFunc("/og-image.svg", s.handleOGImage)
	mux.HandleFunc("/api/auth/session", s.handleAuthSession)
	mux.Handle("/api/users", s.withAPIAuth(http.HandlerFunc(s.handleUsers), permissionManageUsers))
	mux.Handle("/api/users/", s.withAPIAuth(http.HandlerFunc(s.handleUserByID), permissionManageUsers))
	mux.Handle("/api/admin/audit-logs", s.withAPIAuth(http.HandlerFunc(s.handleAuditLogs), permissionManageUsers))
	mux.Handle("/api/admin/memes/pending-delete", s.withAPIAuth(http.HandlerFunc(s.handlePendingDeleteQueue), permissionManageUsers))
	mux.Handle("/api/admin/memes/", s.withAPIAuth(http.HandlerFunc(s.handleAdminMemeActions), permissionManageUsers))
	mux.Handle("/uploads/", s.withPageAuth(http.StripPrefix("/uploads/", http.FileServer(http.Dir(s.managers.UploadDir())))))
	if thumbnailDir := s.managers.ThumbnailDir(); strings.TrimSpace(thumbnailDir) != "" {
		mux.Handle("/thumbnails/", s.withPageAuth(http.StripPrefix("/thumbnails/", http.FileServer(http.Dir(thumbnailDir)))))
	}
	mux.Handle("/static/", s.withPageAuth(http.StripPrefix("/static/", http.FileServer(http.Dir("static")))))
	mux.Handle("/", s.withPageAuth(http.HandlerFunc(s.handleIndex)))
	mux.Handle("/api/memes", s.withAPIAuth(http.HandlerFunc(s.handleMemes), permissionView))
	mux.Handle("/api/memes/", s.withAPIAuth(http.HandlerFunc(s.handleMemeByID), permissionView))
	mux.Handle("/api/memes/random", s.withAPIAuth(http.HandlerFunc(s.handleRandomMeme), permissionView))
	mux.Handle("/api/reel-session", s.withAPIAuth(http.HandlerFunc(s.handleReelSession), permissionView))
	mux.Handle("/api/tags", s.withAPIAuth(http.HandlerFunc(s.handleTags), permissionView))
	return mux
}

func (s *Server) withPageAuth(next http.Handler) http.Handler {
	if !s.auth.enabled() {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, ok := s.auth.sessionFromRequest(r)
		if !ok && isLinkPreviewRequest(r) && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
			next.ServeHTTP(w, r)
			return
		}
		if !ok {
			http.Redirect(w, r, "/auth/login", http.StatusFound)
			return
		}
		if !session.Permissions.CanView && !allowsAuthenticatedShellOnly(r.URL.Path) {
			http.Redirect(w, r, "/forbidden", http.StatusFound)
			return
		}

		next.ServeHTTP(w, r.WithContext(contextWithSession(r.Context(), session)))
	})
}

func allowsAuthenticatedShellOnly(path string) bool {
	switch {
	case path == "/forbidden":
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
		session, ok := s.auth.sessionFromRequest(r)
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
	default:
		return permissions.CanView || permissions.CanUpload || permissions.CanManage
	}
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	refreshToken := strings.TrimSpace(r.URL.Query().Get("refresh"))
	if refreshToken != "" {
		w.Header().Set("Cache-Control", "no-store, max-age=0")
		w.Header().Set("Pragma", "no-cache")
	}

	indexPath := filepath.Join("static", "index.html")
	content, err := os.ReadFile(indexPath)
	if err != nil {
		http.Error(w, "failed to load index", http.StatusInternalServerError)
		return
	}

	replaced := strings.ReplaceAll(string(content), "/static/styles.css", buildAssetURL("/static/styles.css", refreshToken))
	replaced = strings.ReplaceAll(replaced, "/static/app.js", buildAssetURL("/static/app.js", refreshToken))

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(replaced))
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

	page := string(content)
	page = strings.ReplaceAll(page, "{{DISPLAY_NAME}}", html.EscapeString(displayName))
	page = strings.ReplaceAll(page, "{{USERNAME}}", html.EscapeString(username))
	page = strings.ReplaceAll(page, "{{USER_ID}}", html.EscapeString(userID))
	page = strings.ReplaceAll(page, "{{VERSION}}", html.EscapeString(BuildVersion()))

	w.Header().Set("Cache-Control", "no-store, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(page))
}

func buildAssetURL(path string, refreshToken string) string {
	query := url.Values{}
	query.Set("v", BuildVersion())
	if refreshToken != "" {
		query.Set("r", refreshToken)
	}
	return path + "?" + query.Encode()
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

	id := path
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
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

func (s *Server) listMemes(w http.ResponseWriter, r *http.Request) {
	favoritesOnly := r.URL.Query().Get("favorites") == "true"
	tag := r.URL.Query().Get("tag")
	query := r.URL.Query().Get("q")
	view := r.URL.Query().Get("view")
	userID := currentUserID(r)
	offset := parseQueryInt(r, "offset", 0)
	limit := parseQueryInt(r, "limit", 72)

	writeJSON(w, http.StatusOK, s.managers.ListMemes(userID, query, favoritesOnly, tag, view, offset, limit))
}

func (s *Server) createMeme(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(256 << 20); err != nil {
		http.Error(w, "invalid multipart request", http.StatusBadRequest)
		return
	}

	fileHeaders := r.MultipartForm.File["file"]
	if len(fileHeaders) == 0 {
		http.Error(w, "file is required", http.StatusBadRequest)
		return
	}

	created := make([]accessor.Meme, 0, len(fileHeaders))
	duplicates := make([]map[string]any, 0)
	for _, fileHeader := range fileHeaders {
		src, err := fileHeader.Open()
		if err != nil {
			log.Printf("open upload file failed: %v", err)
			http.Error(w, "failed to read upload", http.StatusInternalServerError)
			return
		}

		meme, err := s.managers.CreateMemeAs(
			currentAuditActor(r),
			src,
			fileHeader.Header,
			fileHeader.Filename,
			splitTags(r.FormValue("tags")),
			r.FormValue("notes"),
		)
		src.Close()
		if err != nil {
			var duplicateErr *accessor.DuplicateMemeError
			if errors.As(err, &duplicateErr) {
				duplicates = append(duplicates, map[string]any{
					"filename": fileHeader.Filename,
					"existing": duplicateErr.Existing,
				})
				continue
			}
			log.Printf("create meme failed: %v", err)
			http.Error(w, "failed to save meme", http.StatusInternalServerError)
			return
		}

		created = append(created, meme)
	}

	status := http.StatusCreated
	if len(created) == 0 && len(duplicates) > 0 {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]any{
		"memes":      created,
		"created":    len(created),
		"duplicates": duplicates,
		"skipped":    len(duplicates),
	})
}

func (s *Server) updateMeme(w http.ResponseWriter, r *http.Request, id string) {
	var payload accessor.MemeUpdate
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	session, ok := sessionFromContext(r.Context())
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

	writeJSON(w, http.StatusOK, meme)
}

func (s *Server) updateFavorite(w http.ResponseWriter, r *http.Request, id string) {
	var payload struct {
		Favorite bool `json:"favorite"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	meme, err := s.managers.SetFavorite(currentUserID(r), id, payload.Favorite)
	if errors.Is(err, os.ErrNotExist) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		log.Printf("update favorite failed: %v", err)
		http.Error(w, "failed to update favorite", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, meme)
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

func (s *Server) handlePendingDeleteQueue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	offset := parseQueryInt(r, "offset", 0)
	limit := parseQueryInt(r, "limit", 50)
	records, err := s.managers.ListPendingDeletes(offset, limit)
	if err != nil {
		log.Printf("list pending deletes failed: %v", err)
		http.Error(w, "failed to load pending deletes", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, records)
}

func (s *Server) handleAuditLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	offset := parseQueryInt(r, "offset", 0)
	limit := parseQueryInt(r, "limit", 100)
	events, err := s.managers.ListAuditFeed(offset, limit)
	if err != nil {
		log.Printf("list audit logs failed: %v", err)
		http.Error(w, "failed to load audit logs", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, events)
}

func (s *Server) handleAdminMemeActions(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/admin/memes/")
	if path == "" {
		http.NotFound(w, r)
		return
	}

	switch {
	case r.Method == http.MethodGet && !strings.Contains(path, "/"):
		id := strings.TrimSuffix(path, "/")
		if id == "" {
			http.NotFound(w, r)
			return
		}
		s.handleAdminMemeByID(w, r, id)
	case strings.HasSuffix(path, "/audit"):
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		id := strings.TrimSuffix(strings.TrimSuffix(path, "/audit"), "/")
		s.handleMemeAudit(w, r, id)
	case strings.HasSuffix(path, "/approve-delete"):
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		id := strings.TrimSuffix(strings.TrimSuffix(path, "/approve-delete"), "/")
		if err := s.managers.ApprovePendingDelete(id, currentAuditActor(r)); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				http.NotFound(w, r)
				return
			}
			log.Printf("approve delete failed: %v", err)
			http.Error(w, "failed to approve delete", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case strings.HasSuffix(path, "/reject-delete"):
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		id := strings.TrimSuffix(strings.TrimSuffix(path, "/reject-delete"), "/")
		if err := s.managers.RejectPendingDelete(id, currentAuditActor(r)); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				http.NotFound(w, r)
				return
			}
			log.Printf("reject delete failed: %v", err)
			http.Error(w, "failed to reject delete", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleAdminMemeByID(w http.ResponseWriter, r *http.Request, id string) {
	meme, err := s.managers.GetAdminMeme(id)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.NotFound(w, r)
			return
		}
		log.Printf("get admin meme failed: %v", err)
		http.Error(w, "failed to load meme", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, meme)
}

func (s *Server) handleMemeAudit(w http.ResponseWriter, r *http.Request, id string) {
	limit := parseQueryInt(r, "limit", 100)
	audit, err := s.managers.ListMemeAudit(id, limit)
	if err != nil {
		log.Printf("list meme audit failed: %v", err)
		http.Error(w, "failed to load meme audit", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"events": audit,
	})
}

func (s *Server) handleUsers(w http.ResponseWriter, r *http.Request) {
	if s.users == nil {
		http.Error(w, "user management requires a database", http.StatusNotImplemented)
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.listUsers(w, r)
	case http.MethodPost:
		s.createManagedUser(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleUserByID(w http.ResponseWriter, r *http.Request) {
	if s.users == nil {
		http.Error(w, "user management requires a database", http.StatusNotImplemented)
		return
	}

	userID := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/users/"))
	if userID == "" || strings.Contains(userID, "/") {
		http.NotFound(w, r)
		return
	}

	if _, isSuperAdmin := s.config.DiscordAuth.SuperAdminUserIDs[userID]; isSuperAdmin {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "super admin users are configured from environment variables",
		})
		return
	}

	if r.Method != http.MethodPatch {
		if r.Method == http.MethodDelete {
			s.deleteManagedUser(w, r, userID)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.updateManagedUser(w, r, userID)
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	records, err := s.users.ListUsers(r.Context())
	if err != nil {
		log.Printf("list users failed: %v", err)
		http.Error(w, "failed to load users", http.StatusInternalServerError)
		return
	}

	byID := map[string]managedUserRecord{}
	for _, record := range records {
		normalized := strings.TrimSpace(record.UserID)
		if normalized == "" {
			continue
		}
		record.Permissions = s.auth.permissionsForUser(normalized)
		byID[normalized] = record
	}

	out := make([]map[string]any, 0, len(byID)+len(s.config.DiscordAuth.SuperAdminUserIDs))
	for _, record := range byID {
		_, isSuperAdmin := s.config.DiscordAuth.SuperAdminUserIDs[record.UserID]
		out = append(out, managedUserPayload(record, isSuperAdmin))
	}

	for userID := range s.config.DiscordAuth.SuperAdminUserIDs {
		if _, exists := byID[userID]; exists {
			continue
		}
		out = append(out, managedUserPayload(managedUserRecord{
			UserID:      userID,
			DisplayName: "Super Admin",
			Permissions: s.auth.permissionsForUser(userID),
		}, true))
	}

	writeJSON(w, http.StatusOK, map[string]any{"users": out})
}

func (s *Server) createManagedUser(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		UserID string `json:"user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	userID := strings.TrimSpace(payload.UserID)
	if userID == "" {
		http.Error(w, "user_id is required", http.StatusBadRequest)
		return
	}
	if _, isSuperAdmin := s.config.DiscordAuth.SuperAdminUserIDs[userID]; isSuperAdmin {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "super admin users are configured from environment variables",
		})
		return
	}

	record, err := s.users.CreateUser(r.Context(), userID)
	if err != nil {
		log.Printf("create user failed: %v", err)
		http.Error(w, "failed to create user", http.StatusInternalServerError)
		return
	}
	record.Permissions = s.auth.permissionsForUser(record.UserID)
	writeJSON(w, http.StatusCreated, managedUserPayload(record, false))
}

func (s *Server) updateManagedUser(w http.ResponseWriter, r *http.Request, userID string) {
	var payload struct {
		CanView        bool `json:"canView"`
		CanUpload      bool `json:"canUpload"`
		CanAddTags     bool `json:"canAddTags"`
		CanRemoveTags  bool `json:"canRemoveTags"`
		CanDeleteMemes bool `json:"canDeleteMemes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	permissions := authPermissions{
		CanView:        payload.CanView,
		CanUpload:      payload.CanUpload,
		CanAddTags:     payload.CanAddTags,
		CanRemoveTags:  payload.CanRemoveTags,
		CanDeleteMemes: payload.CanDeleteMemes,
	}
	if permissions.CanUpload || permissions.CanAddTags || permissions.CanRemoveTags || permissions.CanDeleteMemes {
		permissions.CanView = true
	}

	record, err := s.users.UpdateUserPermissions(r.Context(), userID, permissions)
	if err != nil {
		log.Printf("update user failed: %v", err)
		http.Error(w, "failed to update user", http.StatusInternalServerError)
		return
	}
	record.Permissions = s.auth.permissionsForUser(record.UserID)
	writeJSON(w, http.StatusOK, managedUserPayload(record, false))
}

func (s *Server) deleteManagedUser(w http.ResponseWriter, r *http.Request, userID string) {
	if err := s.users.DeleteUser(r.Context(), userID); err != nil {
		log.Printf("delete user failed: %v", err)
		http.Error(w, "failed to delete user", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRandomMeme(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID := r.URL.Query().Get("session_id")
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
		"session_id":       result.SessionID,
		"session_replaced": result.SessionReplaced,
		"reason":           result.Reason,
		"can_go_prev":      result.CanGoPrev,
		"meme":             result.Meme,
	})
}

func (s *Server) handleReelSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID := r.URL.Query().Get("session_id")
	if err := s.managers.DeleteRandomReelSession(sessionID); err != nil {
		log.Printf("delete reel session failed: %v", err)
		http.Error(w, "failed to delete reel session", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !s.auth.enabled() {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	state, err := s.auth.newStateToken()
	if err != nil {
		log.Printf("create oauth state failed: %v", err)
		http.Error(w, "failed to start login", http.StatusInternalServerError)
		return
	}

	s.auth.rememberState(state)
	s.auth.setStateCookie(w, r, state)
	http.Redirect(w, r, s.auth.buildLoginURLForRequest(r, state), http.StatusFound)
}

func (s *Server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	if !s.auth.enabled() {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	defer s.auth.clearCookie(w, r, authStateCookieName)

	if !s.auth.consumeValidState(r) {
		http.Error(w, "invalid oauth state", http.StatusBadRequest)
		return
	}

	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		http.Error(w, "missing oauth code", http.StatusBadRequest)
		return
	}

	redirectURL := s.auth.redirectURLForRequest(r)
	accessToken, err := s.auth.exchangeCode(r.Context(), code, redirectURL)
	if err != nil {
		log.Printf("discord token exchange failed: %v", err)
		http.Error(w, "discord login failed", http.StatusBadGateway)
		return
	}

	user, err := s.auth.fetchUser(r.Context(), accessToken)
	if err != nil {
		log.Printf("discord user lookup failed: %v", err)
		http.Error(w, "discord login failed", http.StatusBadGateway)
		return
	}

	session, token, err := s.auth.createSession(user)
	if err != nil {
		log.Printf("create auth session failed for Discord user %s: %v", strings.TrimSpace(user.ID), err)
		http.Error(w, "discord login failed", http.StatusInternalServerError)
		return
	}
	if !session.Permissions.CanView {
		log.Printf("discord login awaiting approval: user_id=%s username=%s", session.UserID, session.Username)
	}

	s.auth.setSessionCookie(w, r, token, session.ExpiresAt)
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if s.auth.enabled() {
		s.auth.clearCookie(w, r, authSessionCookieName)
	}

	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) handleAuthSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !s.auth.enabled() {
		writeJSON(w, http.StatusOK, map[string]any{
			"enabled": false,
			"version": BuildVersion(),
			"permissions": authPermissions{
				CanView:        true,
				CanUpload:      true,
				CanAddTags:     true,
				CanRemoveTags:  true,
				CanDeleteMemes: true,
				CanManageUsers: true,
				CanAdd:         true,
				CanManage:      true,
			},
		})
		return
	}

	session, ok := s.auth.sessionFromRequest(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"enabled":       true,
			"authenticated": false,
			"login_url":     "/auth/login",
			"version":       BuildVersion(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":       true,
		"authenticated": true,
		"user": map[string]any{
			"id":           session.UserID,
			"username":     session.Username,
			"display_name": session.DisplayName,
			"avatar_url":   session.AvatarURL,
		},
		"permissions": session.Permissions,
		"logout_url":  "/auth/logout",
		"version":     BuildVersion(),
	})
}

func managedUserPayload(record managedUserRecord, isSuperAdmin bool) map[string]any {
	displayName := strings.TrimSpace(record.DisplayName)
	if displayName == "" {
		displayName = strings.TrimSpace(record.Username)
	}

	return map[string]any{
		"user_id":        record.UserID,
		"username":       record.Username,
		"display_name":   displayName,
		"avatar_url":     record.AvatarURL,
		"last_active_at": record.LastActiveAt,
		"permissions":    record.Permissions,
		"is_super_admin": isSuperAdmin,
	}
}

func currentAuditActor(r *http.Request) accessor.AuditActor {
	session, ok := sessionFromContext(r.Context())
	if !ok {
		return accessor.AuditActor{}
	}

	return accessor.AuditActor{
		UserID:       session.UserID,
		Username:     session.Username,
		DisplayName:  session.DisplayName,
		AvatarURL:    session.AvatarURL,
		IsSuperAdmin: session.Permissions.CanManageUsers,
	}
}

func normalizePermissionTags(tags []string) []string {
	out := make([]string, 0, len(tags))
	seen := map[string]struct{}{}
	for _, tag := range tags {
		normalized := strings.ToLower(strings.TrimSpace(tag))
		if normalized == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out
}

func diffTags(current []string, next []string) ([]string, []string) {
	currentSet := map[string]struct{}{}
	nextSet := map[string]struct{}{}
	for _, tag := range current {
		currentSet[tag] = struct{}{}
	}
	for _, tag := range next {
		nextSet[tag] = struct{}{}
	}

	added := []string{}
	removed := []string{}
	for tag := range nextSet {
		if _, exists := currentSet[tag]; !exists {
			added = append(added, tag)
		}
	}
	for tag := range currentSet {
		if _, exists := nextSet[tag]; !exists {
			removed = append(removed, tag)
		}
	}
	return added, removed
}

func contextWithSession(ctx context.Context, session authSession) context.Context {
	return context.WithValue(ctx, authSessionContextKey, session)
}

func currentUserID(r *http.Request) string {
	session, ok := sessionFromContext(r.Context())
	if !ok {
		return ""
	}
	return session.UserID
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("write JSON failed: %v", err)
	}
}

func splitTags(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	return strings.Split(raw, ",")
}

func parseQueryInt(r *http.Request, key string, fallback int) int {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}
