package client

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	authSessionCookieName = "memeindex_session"
	authStateCookieName   = "memeindex_oauth_state"
)

type permissionLevel string

const (
	permissionView   permissionLevel = "view"
	permissionAdd    permissionLevel = "add"
	permissionManage permissionLevel = "manage"
)

type authContextKey string

const authSessionContextKey authContextKey = "auth-session"

type authSession struct {
	ID          string
	UserID      string
	Username    string
	DisplayName string
	AvatarURL   string
	Permissions authPermissions
	ExpiresAt   time.Time
}

type authPermissions struct {
	CanView   bool `json:"canView"`
	CanAdd    bool `json:"canAdd"`
	CanManage bool `json:"canManage"`
}

type authService struct {
	config        DiscordAuthConfig
	secret        []byte
	client        *http.Client
	sessions      map[string]authSession
	pendingStates map[string]time.Time
	mu            sync.RWMutex
}

type discordTokenResponse struct {
	AccessToken string `json:"access_token"`
}

type discordUser struct {
	ID            string `json:"id"`
	Username      string `json:"username"`
	GlobalName    string `json:"global_name"`
	Avatar        string `json:"avatar"`
	Discriminator string `json:"discriminator"`
}

func newAuthService(config DiscordAuthConfig) *authService {
	if !config.Enabled() {
		return nil
	}

	return &authService{
		config:        config,
		secret:        []byte(config.SessionSecret),
		client:        &http.Client{Timeout: 15 * time.Second},
		sessions:      map[string]authSession{},
		pendingStates: map[string]time.Time{},
	}
}

func (a *authService) enabled() bool {
	return a != nil
}

func (a *authService) buildLoginURL(state string) string {
	values := url.Values{}
	values.Set("client_id", a.config.ClientID)
	values.Set("response_type", "code")
	values.Set("redirect_uri", a.config.RedirectURL)
	values.Set("scope", "identify")
	values.Set("state", state)
	return "https://discord.com/oauth2/authorize?" + values.Encode()
}

func (a *authService) buildLoginURLForRequest(r *http.Request, state string) string {
	values := url.Values{}
	values.Set("client_id", a.config.ClientID)
	values.Set("response_type", "code")
	values.Set("redirect_uri", a.redirectURLForRequest(r))
	values.Set("scope", "identify")
	values.Set("state", state)
	return "https://discord.com/oauth2/authorize?" + values.Encode()
}

func (a *authService) redirectURLForRequest(r *http.Request) string {
	if !a.config.DynamicRedirect {
		return a.config.RedirectURL
	}

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
		host = strings.TrimSpace(r.Host)
	}

	return (&url.URL{
		Scheme: scheme,
		Host:   host,
		Path:   "/auth/callback",
	}).String()
}

func (a *authService) newStateToken() (string, error) {
	return a.randomToken(32)
}

func (a *authService) newSessionID() (string, error) {
	return a.randomToken(32)
}

func (a *authService) randomToken(size int) (string, error) {
	bytes := make([]byte, size)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func (a *authService) sign(value string) string {
	mac := hmac.New(sha256.New, a.secret)
	mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil))
}

func (a *authService) signedValue(value string) string {
	return value + "." + a.sign(value)
}

func (a *authService) verifySignedValue(signed string) (string, bool) {
	parts := strings.Split(signed, ".")
	if len(parts) != 2 {
		return "", false
	}
	value := parts[0]
	signature := parts[1]
	if !hmac.Equal([]byte(signature), []byte(a.sign(value))) {
		return "", false
	}
	return value, true
}

func (a *authService) cookieSecureForRequest(r *http.Request) bool {
	if !a.config.CookieSecure {
		return false
	}

	scheme := forwardedHeaderFirstValue(r.Header.Get("X-Forwarded-Proto"))
	if scheme != "" {
		return strings.EqualFold(scheme, "https")
	}

	return r.TLS != nil
}

func (a *authService) setCookie(w http.ResponseWriter, r *http.Request, name, value string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    base64.RawURLEncoding.EncodeToString([]byte(value)),
		Path:     "/",
		HttpOnly: true,
		Secure:   a.cookieSecureForRequest(r),
		SameSite: http.SameSiteLaxMode,
		Expires:  expires,
	})
}

func (a *authService) clearCookie(w http.ResponseWriter, r *http.Request, name string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   a.cookieSecureForRequest(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}

func (a *authService) readCookie(r *http.Request, name string) (string, bool) {
	cookie, err := r.Cookie(name)
	if err != nil {
		return "", false
	}

	decoded, err := base64.RawURLEncoding.DecodeString(cookie.Value)
	if err != nil {
		return "", false
	}
	return string(decoded), true
}

func (a *authService) setStateCookie(w http.ResponseWriter, r *http.Request, state string) {
	a.setCookie(w, r, authStateCookieName, a.signedValue(state), time.Now().Add(10*time.Minute))
}

func (a *authService) rememberState(state string) {
	if strings.TrimSpace(state) == "" {
		return
	}

	expiresAt := time.Now().Add(10 * time.Minute)

	a.mu.Lock()
	defer a.mu.Unlock()

	for existingState, existingExpiresAt := range a.pendingStates {
		if time.Now().After(existingExpiresAt) {
			delete(a.pendingStates, existingState)
		}
	}

	a.pendingStates[state] = expiresAt
}

func (a *authService) consumeValidState(r *http.Request) bool {
	queryState := strings.TrimSpace(r.URL.Query().Get("state"))
	if queryState == "" {
		return false
	}

	now := time.Now()

	a.mu.Lock()
	expiresAt, ok := a.pendingStates[queryState]
	if ok {
		delete(a.pendingStates, queryState)
	}
	for existingState, existingExpiresAt := range a.pendingStates {
		if now.After(existingExpiresAt) {
			delete(a.pendingStates, existingState)
		}
	}
	a.mu.Unlock()

	if ok && now.Before(expiresAt) {
		return true
	}

	signedState, cookieOK := a.readCookie(r, authStateCookieName)
	if !cookieOK {
		return false
	}

	value, verifyOK := a.verifySignedValue(signedState)
	return verifyOK && value == queryState
}

func (a *authService) setSessionCookie(w http.ResponseWriter, r *http.Request, sessionID string, expires time.Time) {
	a.setCookie(w, r, authSessionCookieName, a.signedValue(sessionID), expires)
}

func (a *authService) sessionFromRequest(r *http.Request) (authSession, bool) {
	signedSession, ok := a.readCookie(r, authSessionCookieName)
	if !ok {
		return authSession{}, false
	}

	sessionID, ok := a.verifySignedValue(signedSession)
	if !ok {
		return authSession{}, false
	}

	a.mu.RLock()
	session, exists := a.sessions[sessionID]
	a.mu.RUnlock()
	if !exists {
		return authSession{}, false
	}

	if time.Now().After(session.ExpiresAt) {
		a.mu.Lock()
		delete(a.sessions, sessionID)
		a.mu.Unlock()
		return authSession{}, false
	}

	return session, true
}

func (a *authService) createSession(user discordUser) (authSession, error) {
	permissions := a.permissionsForUser(user.ID)
	if !permissions.CanView {
		return authSession{}, errors.New("user is not authorized")
	}

	sessionID, err := a.newSessionID()
	if err != nil {
		return authSession{}, err
	}

	displayName := strings.TrimSpace(user.GlobalName)
	if displayName == "" {
		displayName = user.Username
	}

	session := authSession{
		ID:          sessionID,
		UserID:      user.ID,
		Username:    user.Username,
		DisplayName: displayName,
		AvatarURL:   discordAvatarURL(user),
		Permissions: permissions,
		ExpiresAt:   time.Now().Add(7 * 24 * time.Hour),
	}

	a.mu.Lock()
	a.sessions[sessionID] = session
	a.mu.Unlock()
	return session, nil
}

func discordAvatarURL(user discordUser) string {
	if user.Avatar == "" {
		return ""
	}
	return fmt.Sprintf("https://cdn.discordapp.com/avatars/%s/%s.png", user.ID, user.Avatar)
}

func (a *authService) permissionsForUser(userID string) authPermissions {
	_, canManage := a.config.ManageUserIDs[userID]
	_, canAdd := a.config.AddUserIDs[userID]
	_, canView := a.config.ViewUserIDs[userID]

	if canManage {
		canAdd = true
		canView = true
	}
	if canAdd {
		canView = true
	}

	return authPermissions{
		CanView:   canView,
		CanAdd:    canAdd,
		CanManage: canManage,
	}
}

func (a *authService) exchangeCode(ctx context.Context, code string, redirectURL string) (string, error) {
	values := url.Values{}
	values.Set("client_id", a.config.ClientID)
	values.Set("client_secret", a.config.ClientSecret)
	values.Set("grant_type", "authorization_code")
	values.Set("code", code)
	values.Set("redirect_uri", redirectURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://discord.com/api/oauth2/token", strings.NewReader(values.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := a.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("discord token exchange failed: %s", strings.TrimSpace(string(body)))
	}

	var payload discordTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	if strings.TrimSpace(payload.AccessToken) == "" {
		return "", errors.New("discord token exchange returned no access token")
	}
	return payload.AccessToken, nil
}

func (a *authService) fetchUser(ctx context.Context, accessToken string) (discordUser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://discord.com/api/users/@me", nil)
	if err != nil {
		return discordUser{}, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := a.client.Do(req)
	if err != nil {
		return discordUser{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return discordUser{}, fmt.Errorf("discord user fetch failed: %s", strings.TrimSpace(string(body)))
	}

	var user discordUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return discordUser{}, err
	}
	if strings.TrimSpace(user.ID) == "" {
		return discordUser{}, errors.New("discord user payload was missing id")
	}
	return user, nil
}

func sessionFromContext(ctx context.Context) (authSession, bool) {
	session, ok := ctx.Value(authSessionContextKey).(authSession)
	return session, ok
}

func forwardedHeaderFirstValue(value string) string {
	if value == "" {
		return ""
	}
	parts := strings.Split(value, ",")
	return strings.TrimSpace(parts[0])
}
