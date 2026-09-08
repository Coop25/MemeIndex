package client

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// While a portable-backup restore holds the maintenance lock, mutating requests
// must be turned away with 503 and reads must keep working. The restore endpoint
// itself stays reachable so an in-progress import can finish.
func TestRestoreMaintenanceLockBlocksMutations(t *testing.T) {
	s, _, _ := newShareTestServer(t)
	s.auth = newAuthService(DiscordAuthConfig{}, nil) // anonymous: auth middleware is a pass-through
	handler := s.Routes()

	do := func(method, path string) int {
		r := httptest.NewRequest(method, "https://example.com"+path, strings.NewReader(`{}`))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Code
	}

	// Baseline: mutation allowed through to its handler before a restore starts.
	if code := do(http.MethodPost, "/api/tags"); code == http.StatusServiceUnavailable {
		t.Fatalf("mutation blocked before restore lock was held: %d", code)
	}

	s.restoring.Store(true)
	defer s.restoring.Store(false)

	if code := do(http.MethodPost, "/api/tags"); code != http.StatusServiceUnavailable {
		t.Fatalf("POST during restore: got %d, want 503", code)
	}
	if code := do(http.MethodDelete, "/api/memes/anything"); code != http.StatusServiceUnavailable {
		t.Fatalf("DELETE during restore: got %d, want 503", code)
	}
	if code := do(http.MethodGet, "/healthz"); code != http.StatusOK {
		t.Fatalf("GET during restore: got %d, want 200", code)
	}
	if code := do(http.MethodGet, "/api/tags"); code == http.StatusServiceUnavailable {
		t.Fatal("read request blocked during restore")
	}
	// The import route is exempt so the running restore is never locked out of
	// itself; here it falls through to a non-503 handler response.
	if code := do(http.MethodPost, "/api/admin/backup/import"); code == http.StatusServiceUnavailable {
		t.Fatal("backup import route was blocked by its own maintenance lock")
	}
}

// A second concurrent import is refused with 409 rather than corrupting a
// half-restored server.
func TestRestoreMaintenanceLockRejectsSecondImport(t *testing.T) {
	s, _, _ := newShareTestServer(t)
	s.backup = &portableBackup{dataDir: t.TempDir(), job: backupJobStatus{State: "idle"}}
	s.restoring.Store(true)
	defer s.restoring.Store(false)

	r := httptest.NewRequest(http.MethodPost, "https://example.com/api/admin/backup/import", strings.NewReader("archive"))
	w := httptest.NewRecorder()
	s.handleBackupImport(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("second import: got %d, want 409", w.Code)
	}
}
