package client

import (
	"log"
	"net/http"
	"strings"
	"time"
)

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

	if !s.auth.rememberState(state) {
		w.Header().Set("Retry-After", "60")
		http.Error(w, "too many pending logins; try again shortly", http.StatusTooManyRequests)
		return
	}
	s.auth.setStateCookie(w, r, state)
	if returnTo := safeLocalReturnPath(r.URL.Query().Get("return_to")); returnTo != "" {
		s.auth.setCookie(w, r, authReturnCookieName, s.auth.signedValue(returnTo), time.Now().Add(10*time.Minute))
	}
	http.Redirect(w, r, s.auth.buildLoginURLForRequest(r, state), http.StatusFound)
}

func (s *Server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	if !s.auth.enabled() {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	// Set deletion headers before any error or redirect commits the response.
	s.auth.clearCookie(w, r, authStateCookieName)
	s.auth.clearCookie(w, r, authReturnCookieName)

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
	returnTo := "/"
	if signedReturn, ok := s.auth.readCookie(r, authReturnCookieName); ok {
		if value, valid := s.auth.verifySignedValue(signedReturn); valid {
			if safe := safeLocalReturnPath(value); safe != "" {
				returnTo = safe
			}
		}
	}
	http.Redirect(w, r, returnTo, http.StatusFound)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	// POST only: logout revokes every session the user has, so letting a plain
	// GET do it meant any link on any site could sign someone out of all their
	// devices. A stray GET (an old bookmark) just goes back to the app.
	// Cross-site POSTs are already refused by CrossOriginProtection.
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	if s.auth.enabled() {
		// Advance the user's session version so the signed token being discarded
		// here (and any copy of it that outlived this browser) stops validating
		// immediately instead of lingering until its exp claim.
		if session, ok := s.auth.sessionFromRequest(r); ok {
			if err := s.auth.revokeSessions(r.Context(), session.UserID); err != nil {
				log.Printf("revoke sessions on logout failed for user %s: %v", session.UserID, err)
			}
		}
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
			"enabled":                      false,
			"version":                      BuildVersion(),
			"audiobook_upload_concurrency": s.config.AudiobookUploadConcurrency,
			"audiobooks_available":         s.audiobooks != nil,
			"public_base_url":              s.publicOrigin(r),
			"permissions": authPermissions{
				CanView:                  true,
				CanUpload:                true,
				CanAddTags:               true,
				CanRemoveTags:            true,
				CanDeleteMemes:           true,
				CanManageUsers:           true,
				CanViewAudiobooks:        true,
				CanUploadAudiobooks:      true,
				CanDeleteAudiobooks:      true,
				CanManageAudiobookSeries: true,
				CanAdd:                   true,
				CanManage:                true,
			},
		})
		return
	}

	session, ok, err := s.auth.resolveSession(r)
	if err != nil {
		writeSessionUnavailable(w)
		return
	}
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"enabled":                      true,
			"authenticated":                false,
			"login_url":                    "/auth/login",
			"version":                      BuildVersion(),
			"audiobook_upload_concurrency": s.config.AudiobookUploadConcurrency,
			"audiobooks_available":         s.audiobooks != nil,
			"public_base_url":              s.publicOrigin(r),
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
		"permissions":                  session.Permissions,
		"logout_url":                   "/auth/logout",
		"version":                      BuildVersion(),
		"audiobook_upload_concurrency": s.config.AudiobookUploadConcurrency,
		"audiobooks_available":         s.audiobooks != nil,
		"public_base_url":              s.publicOrigin(r),
	})
}
