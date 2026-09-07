package client

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRoutesSetBaselineContentSecurityPolicy(t *testing.T) {
	server, _, _ := newShareTestServer(t)
	handler := server.Routes()

	t.Run("first-party pages get the locked-down app policy", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "https://example.com/service-worker.js", nil))

		csp := recorder.Header().Get("Content-Security-Policy")
		for _, directive := range []string{
			"script-src 'self'",
			"object-src 'none'",
			"base-uri 'none'",
			"frame-ancestors 'none'",
		} {
			if !strings.Contains(csp, directive) {
				t.Fatalf("app CSP %q missing %q", csp, directive)
			}
		}
		if strings.Contains(csp, "'unsafe-eval'") || strings.Contains(csp, "script-src 'unsafe-inline'") {
			t.Fatalf("app CSP unexpectedly permits inline/eval scripts: %q", csp)
		}
	})

	t.Run("api responses forbid loading any resource", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "https://example.com/api/auth/session", nil))

		if got := recorder.Header().Get("Content-Security-Policy"); got != apiContentSecurityPolicy {
			t.Fatalf("api CSP = %q, want %q", got, apiContentSecurityPolicy)
		}
	})
}

func TestAccessDeniedPagePinsInlineScriptToNonce(t *testing.T) {
	template, err := os.ReadFile(filepath.Join("..", "..", "static", "access-denied.html"))
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	if err := os.Mkdir("static", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("static", "access-denied.html"), template, 0o644); err != nil {
		t.Fatal(err)
	}

	server := &Server{}
	recorder := httptest.NewRecorder()
	server.handleAccessDeniedPage(recorder, httptest.NewRequest(http.MethodGet, "https://example.com/forbidden", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	body := recorder.Body.String()
	if strings.Contains(body, "{{SCRIPT_NONCE}}") {
		t.Fatal("script nonce placeholder was not replaced")
	}

	csp := recorder.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "script-src 'nonce-") {
		t.Fatalf("access-denied CSP %q is missing a script nonce", csp)
	}
	if strings.Contains(csp, "script-src 'unsafe-inline'") || strings.Contains(csp, "script-src 'self'") {
		t.Fatalf("access-denied CSP %q must not broadly allow scripts", csp)
	}

	start := strings.Index(csp, "script-src 'nonce-") + len("script-src 'nonce-")
	end := strings.IndexByte(csp[start:], '\'')
	if end <= 0 {
		t.Fatalf("could not parse nonce out of CSP %q", csp)
	}
	nonce := csp[start : start+end]
	if !strings.Contains(body, `<script nonce="`+nonce+`">`) {
		t.Fatalf("inline <script> tag is not tagged with CSP nonce %q", nonce)
	}
}
