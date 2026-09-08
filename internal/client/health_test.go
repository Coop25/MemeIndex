package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"memeindex/internal/accessor"
	"memeindex/internal/manager"
)

// pingableStore wraps the file-backed store with a Ping so the /readyz datastore
// branch can be exercised without a live PostgreSQL connection.
type pingableStore struct {
	*accessor.MemeStore
	pingErr error
}

func (p pingableStore) Ping(context.Context) error { return p.pingErr }

func newHealthTestServer(t *testing.T, wrap func(*accessor.MemeStore) accessor.Store) *Server {
	t.Helper()
	base, err := accessor.NewMemeStore(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	var store accessor.Store = base
	if wrap != nil {
		store = wrap(base)
	}
	return &Server{managers: manager.NewMemeManager(store)}
}

func decodeHealthBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body.String(), err)
	}
	return body
}

func TestHandleHealthzReportsOK(t *testing.T) {
	s := &Server{}
	rec := httptest.NewRecorder()
	s.handleHealthz(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store, max-age=0" {
		t.Fatalf("cache-control = %q", got)
	}
	if body := decodeHealthBody(t, rec); body["status"] != "ok" {
		t.Fatalf("status field = %v", body["status"])
	}
}

func TestHandleHealthzRejectsNonGET(t *testing.T) {
	s := &Server{}
	rec := httptest.NewRecorder()
	s.handleHealthz(rec, httptest.NewRequest(http.MethodPost, "/healthz", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestHandleReadyzSkipsDatastoreCheckWithoutPinger(t *testing.T) {
	s := newHealthTestServer(t, nil)
	rec := httptest.NewRecorder()
	s.handleReadyz(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := decodeHealthBody(t, rec)
	if body["status"] != "ready" {
		t.Fatalf("status field = %v", body["status"])
	}
	checks, _ := body["checks"].(map[string]any)
	if checks["datastore"] != "skipped" {
		t.Fatalf("datastore check = %v, want skipped", checks["datastore"])
	}
}

func TestHandleReadyzOKWhenDatastorePingSucceeds(t *testing.T) {
	s := newHealthTestServer(t, func(base *accessor.MemeStore) accessor.Store {
		return pingableStore{MemeStore: base}
	})
	rec := httptest.NewRecorder()
	s.handleReadyz(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	checks, _ := decodeHealthBody(t, rec)["checks"].(map[string]any)
	if checks["datastore"] != "ok" {
		t.Fatalf("datastore check = %v, want ok", checks["datastore"])
	}
}

func TestHandleReadyzUnavailableWhenDatastorePingFails(t *testing.T) {
	s := newHealthTestServer(t, func(base *accessor.MemeStore) accessor.Store {
		return pingableStore{MemeStore: base, pingErr: errors.New("connection refused")}
	})
	rec := httptest.NewRecorder()
	s.handleReadyz(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	body := decodeHealthBody(t, rec)
	if body["status"] != "not_ready" {
		t.Fatalf("status field = %v", body["status"])
	}
	checks, _ := body["checks"].(map[string]any)
	if checks["datastore"] != "unreachable" {
		t.Fatalf("datastore check = %v, want unreachable", checks["datastore"])
	}
}

func TestHandleReadyzUnavailableWhileDraining(t *testing.T) {
	s := newHealthTestServer(t, nil)
	s.BeginDraining()
	rec := httptest.NewRecorder()
	s.handleReadyz(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	checks, _ := decodeHealthBody(t, rec)["checks"].(map[string]any)
	if checks["accepting_traffic"] != "draining" {
		t.Fatalf("accepting_traffic check = %v, want draining", checks["accepting_traffic"])
	}
}

func TestRoutesExposeHealthEndpointsWithoutAuth(t *testing.T) {
	s := newHealthTestServer(t, nil)
	s.auth = newAuthService(DiscordAuthConfig{
		ClientID:      "id",
		ClientSecret:  "secret",
		RedirectURL:   "https://example.com/auth/callback",
		SessionSecret: "0123456789abcdef0123456789abcdef",
	}, nil)
	if !s.auth.enabled() {
		t.Fatal("auth service should be enabled for this test")
	}
	s.shareSecret = []byte("0123456789abcdef0123456789abcdef")

	handler := s.Routes()
	for _, path := range []string{"/healthz", "/readyz"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusFound {
			t.Fatalf("%s returned %d; health probes must not require auth", path, rec.Code)
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("%s returned %d, want 200", path, rec.Code)
		}
	}
}
