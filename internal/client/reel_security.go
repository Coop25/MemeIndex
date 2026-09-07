package client

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strings"
)

func (s *Server) reelToken(r *http.Request, id string) string {
	if !s.auth.enabled() {
		return id
	}
	mac := hmac.New(sha256.New, s.shareSecret)
	_, _ = mac.Write([]byte("reel-session\n" + currentUserID(r) + "\n" + id))
	return id + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *Server) reelID(r *http.Request, token string) (string, bool) {
	if token == "" {
		return "", true
	}
	if !s.auth.enabled() {
		return token, true
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 || parts[0] == "" || len(s.shareSecret) == 0 {
		return "", false
	}
	return parts[0], hmac.Equal([]byte(token), []byte(s.reelToken(r, parts[0])))
}
