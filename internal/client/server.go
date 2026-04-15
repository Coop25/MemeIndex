package client

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
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
}

func NewServer(config Config, memeManager *manager.MemeManager) *Server {
	return &Server{
		config:   config,
		managers: memeManager,
		auth:     newAuthService(config.DiscordAuth),
	}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/login", s.handleLogin)
	mux.HandleFunc("/auth/callback", s.handleOAuthCallback)
	mux.HandleFunc("/auth/logout", s.handleLogout)
	mux.HandleFunc("/api/auth/session", s.handleAuthSession)
	mux.Handle("/uploads/", s.withPageAuth(http.StripPrefix("/uploads/", http.FileServer(http.Dir(s.managers.UploadDir())))))
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
		if !ok || !session.Permissions.CanView {
			http.Redirect(w, r, "/auth/login", http.StatusFound)
			return
		}

		next.ServeHTTP(w, r.WithContext(contextWithSession(r.Context(), session)))
	})
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
	case permissionManage:
		return permissions.CanManage
	case permissionAdd:
		return permissions.CanAdd || permissions.CanManage
	default:
		return permissions.CanView || permissions.CanAdd || permissions.CanManage
	}
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	http.ServeFile(w, r, filepath.Join("static", "index.html"))
}

func (s *Server) handleMemes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listMemes(w, r)
	case http.MethodPost:
		if !s.requirePermission(w, r, permissionAdd) {
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
		if !s.requirePermission(w, r, permissionManage) {
			return
		}
		s.updateMeme(w, r, id)
	case http.MethodDelete:
		if !s.requirePermission(w, r, permissionManage) {
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

		meme, err := s.managers.CreateMeme(
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

	meme, err := s.managers.UpdateMeme(currentUserID(r), id, payload)
	if errors.Is(err, os.ErrNotExist) {
		http.NotFound(w, r)
		return
	}
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
	err := s.managers.DeleteMeme(id)
	if errors.Is(err, os.ErrNotExist) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		log.Printf("delete meme failed: %v", err)
		http.Error(w, "failed to delete meme", http.StatusInternalServerError)
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
		http.Error(w, "your Discord account is not authorized for MemeIndex", http.StatusForbidden)
		return
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
			"permissions": authPermissions{
				CanView:   true,
				CanAdd:    true,
				CanManage: true,
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
	})
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
