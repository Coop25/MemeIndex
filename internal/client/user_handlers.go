package client

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
)

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
		if s.auth.enabled() {
			record.Permissions = s.auth.permissionsForUser(normalized)
		}
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
	// A previously denied visitor may have a cached zero-permission session.
	// Anonymous deployments have no auth service (and no sessions to drop or
	// policy to apply) at all - s.auth is nil there, per s.auth.enabled().
	if s.auth.enabled() {
		s.auth.invalidateUser(record.UserID)
		record.Permissions = s.auth.permissionsForUser(record.UserID)
	}
	writeJSON(w, http.StatusCreated, managedUserPayload(record, false))
}

func (s *Server) updateManagedUser(w http.ResponseWriter, r *http.Request, userID string) {
	var payload struct {
		CanView                  bool `json:"canView"`
		CanUpload                bool `json:"canUpload"`
		CanAddTags               bool `json:"canAddTags"`
		CanRemoveTags            bool `json:"canRemoveTags"`
		CanDeleteMemes           bool `json:"canDeleteMemes"`
		CanViewAudiobooks        bool `json:"canViewAudiobooks"`
		CanUploadAudiobooks      bool `json:"canUploadAudiobooks"`
		CanDeleteAudiobooks      bool `json:"canDeleteAudiobooks"`
		CanManageAudiobookSeries bool `json:"canManageAudiobookSeries"`
		// AudiobookTagRestriction locks this account to only the audiobooks
		// carrying at least one of these tags; empty (including omitted)
		// means unrestricted. Always sent in full by the admin UI, same as
		// every CanXxx field above, so there's no partial-update ambiguity.
		AudiobookTagRestriction []string `json:"audiobookTagRestriction"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if len(payload.AudiobookTagRestriction) > 50 {
		http.Error(w, "too many tags", http.StatusBadRequest)
		return
	}

	permissions := authPermissions{
		CanView:                  payload.CanView,
		CanUpload:                payload.CanUpload,
		CanAddTags:               payload.CanAddTags,
		CanRemoveTags:            payload.CanRemoveTags,
		CanDeleteMemes:           payload.CanDeleteMemes,
		CanViewAudiobooks:        payload.CanViewAudiobooks,
		CanUploadAudiobooks:      payload.CanUploadAudiobooks,
		CanDeleteAudiobooks:      payload.CanDeleteAudiobooks,
		CanManageAudiobookSeries: payload.CanManageAudiobookSeries,
	}
	if permissions.CanUpload || permissions.CanAddTags || permissions.CanRemoveTags || permissions.CanDeleteMemes {
		permissions.CanView = true
	}
	if permissions.CanUploadAudiobooks || permissions.CanDeleteAudiobooks || permissions.CanManageAudiobookSeries {
		permissions.CanViewAudiobooks = true
	}

	record, err := s.users.UpdateUserPermissions(r.Context(), userID, permissions)
	if err != nil {
		log.Printf("update user failed: %v", err)
		http.Error(w, "failed to update user", http.StatusInternalServerError)
		return
	}
	record, err = s.users.SetUserAudiobookTagRestriction(r.Context(), userID, payload.AudiobookTagRestriction)
	if err != nil {
		log.Printf("set audiobook tag restriction failed: %v", err)
		http.Error(w, "failed to update user", http.StatusInternalServerError)
		return
	}
	// Drop any cached session so the new permissions take effect on the next
	// request instead of after the cache TTL. Anonymous deployments have no
	// auth service at all - s.auth is nil there, per s.auth.enabled().
	if s.auth.enabled() {
		s.auth.invalidateUser(record.UserID)
		record.Permissions = s.auth.permissionsForUser(record.UserID)
	}
	writeJSON(w, http.StatusOK, managedUserPayload(record, false))
}

func (s *Server) deleteManagedUser(w http.ResponseWriter, r *http.Request, userID string) {
	if err := s.users.DeleteUser(r.Context(), userID); err != nil {
		log.Printf("delete user failed: %v", err)
		http.Error(w, "failed to delete user", http.StatusInternalServerError)
		return
	}
	// Stop honouring any cached session for the now-deleted account at once.
	if s.auth.enabled() {
		s.auth.invalidateUser(userID)
	}

	w.WriteHeader(http.StatusNoContent)
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
