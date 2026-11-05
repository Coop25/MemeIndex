package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAdminDashboardWithoutPostgres(t *testing.T) {
	s, _, _ := newShareTestServer(t)
	s.config.DataDir = t.TempDir()
	s.auth = newAuthService(DiscordAuthConfig{ClientID: "c", ClientSecret: "s", RedirectURL: "https://example.com/auth/callback", SessionSecret: "test"}, &securityUsers{permissions: authPermissions{CanView: true, CanManageUsers: true}})
	token, err := s.auth.issueSessionToken(authSession{UserID: "admin", SessionVersion: 1, ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "https://example.com/api/admin/dashboard", nil)
	w := httptest.NewRecorder()
	s.auth.setSessionCookie(w, r, token, time.Now().Add(time.Hour))
	r.AddCookie(w.Result().Cookies()[0])
	w = httptest.NewRecorder()
	s.Routes().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if _, ok := body["audiobook_stats"]; ok {
		t.Fatal("audiobook_stats present without postgres")
	}
	if string(body["total_storage_bytes"]) != string(body["total_size_bytes"]) {
		t.Fatalf("total_storage_bytes %s != total_size_bytes %s", body["total_storage_bytes"], body["total_size_bytes"])
	}
	var health []adminSystemHealth
	if err := json.Unmarshal(body["system_health"], &health); err != nil {
		t.Fatal(err)
	}
	seen := map[string]adminSystemHealth{}
	for _, h := range health {
		seen[h.Name] = h
	}
	for _, name := range []string{"Disk", "Last Backup"} {
		if _, ok := seen[name]; !ok {
			t.Errorf("missing health row %q in %+v", name, health)
		}
	}
	if seen["Last Backup"].Status != "None yet" {
		t.Errorf("Last Backup = %+v", seen["Last Backup"])
	}
}
