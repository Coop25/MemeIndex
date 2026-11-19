package client

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func signedValueTestAuth(secret string) *authService {
	return newAuthService(DiscordAuthConfig{
		ClientID:        "client",
		ClientSecret:    "secret",
		RedirectURL:     "https://memes.example.com/auth/callback",
		SessionSecret:   secret,
		SessionDuration: 24 * time.Hour,
	}, nil)
}

// The post-login return path is a signed cookie value, and return paths carry
// dots (file-like paths, search terms, version strings) that used to make
// verification split the value apart and silently drop it.
func TestSignedValueRoundTripsValuesContainingDots(t *testing.T) {
	a := signedValueTestAuth("test-session-secret-0123456789abcdef")
	for _, value := range []string{
		"/",
		"/m/abc",
		"/?q=foo.bar",
		"/m/abc?tab=x.y",
		"/m/a.b.c?q=1.2.3&next=%2Fx.y#frag.ment",
		"/uploads/0190a0b8-1234.mp4",
		".",
		"..",
		"/trailing.",
		"",
		"0123456789abcdef0123456789abcdef", // an OAuth state token
	} {
		got, ok := a.verifySignedValue(a.signedValue(value))
		if !ok || got != value {
			t.Errorf("verifySignedValue(signedValue(%q)) = %q, %v; want the value back", value, got, ok)
		}
	}
}

func TestSignedValueRejectsTampering(t *testing.T) {
	a := signedValueTestAuth("test-session-secret-0123456789abcdef")
	signed := a.signedValue("/?q=foo.bar")
	dot := strings.LastIndexByte(signed, '.')
	value, signature := signed[:dot], signed[dot+1:]

	flipped := []byte(signature)
	if flipped[0] == '0' {
		flipped[0] = '1'
	} else {
		flipped[0] = '0'
	}
	other := signedValueTestAuth("another-session-secret-0123456789ab")
	for name, candidate := range map[string]string{
		"flipped signature":           value + "." + string(flipped),
		"changed value":               "/?q=foo.baz." + signature,
		"value cut at an inner dot":   "/?q=foo." + signature,
		"truncated signature":         value + "." + signature[:len(signature)-1],
		"extra segment after the sig": signed + ".00",
		"no signature":                value,
		"empty signature":             value + ".",
		"signature only":              "." + signature,
		"empty":                       "",
		"signed with another secret":  other.signedValue("/?q=foo.bar"),
	} {
		if got, ok := a.verifySignedValue(candidate); ok {
			t.Errorf("%s: verifySignedValue(%q) accepted it as %q", name, candidate, got)
		}
	}
}

// End to end through the login handler's cookie: the return path set by
// /auth/login is the one the callback reads back.
func TestLoginReturnCookieKeepsDottedReturnPath(t *testing.T) {
	server := &Server{auth: signedValueTestAuth("test-session-secret-0123456789abcdef")}
	const returnTo = "/m/abc?tab=x.y&q=foo.bar"
	login := httptest.NewRequest(http.MethodGet, "/auth/login?return_to="+url.QueryEscape(returnTo), nil)
	rec := httptest.NewRecorder()
	server.handleLogin(rec, login)
	if rec.Code != http.StatusFound {
		t.Fatalf("login status = %d, want 302", rec.Code)
	}

	callback := httptest.NewRequest(http.MethodGet, "/auth/callback", nil)
	for _, cookie := range rec.Result().Cookies() {
		callback.AddCookie(cookie)
	}
	signedReturn, ok := server.auth.readCookie(callback, authReturnCookieName)
	if !ok {
		t.Fatal("login did not set the return-path cookie")
	}
	value, valid := server.auth.verifySignedValue(signedReturn)
	if !valid || safeLocalReturnPath(value) != returnTo {
		t.Fatalf("return path read back as %q (valid %v), want %q", value, valid, returnTo)
	}
}
