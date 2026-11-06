package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
	if seen["Last Backup"].Status != "Requires PostgreSQL" || seen["Last Backup"].Healthy {
		t.Errorf("Last Backup = %+v, want unhealthy Requires PostgreSQL", seen["Last Backup"])
	}
	disk := seen["Disk"]
	wantDiskHealthy := disk.Status != "Unknown" && !strings.HasPrefix(disk.Status, "Low:")
	if disk.Healthy != wantDiskHealthy {
		t.Errorf("Disk = %+v, want Healthy=%v", disk, wantDiskHealthy)
	}
	if _, ok := seen["Audiobooks"]; ok {
		t.Errorf("Audiobooks health row present without postgres: %+v", health)
	}
}

// dashboardStore serves configurable audiobook aggregates.
type dashboardStore struct {
	audiobookStore
	statsErr     bool
	listeningErr bool
	healthErr    bool
	mp3sErr      bool
	mp3s         []audiobookStoredMP3
	awaiting     int
}

func (d dashboardStore) AdminStats(context.Context, time.Time) (adminAudiobookStats, error) {
	if d.statsErr {
		return adminAudiobookStats{}, os.ErrInvalid
	}
	return adminAudiobookStats{}, nil
}

func (d dashboardStore) AdminListening(context.Context, time.Time) ([]adminDaySeconds, []adminTopBook, []adminTopListener, error) {
	if d.listeningErr {
		return nil, nil, nil, os.ErrInvalid
	}
	return []adminDaySeconds{}, []adminTopBook{}, []adminTopListener{}, nil
}

func (d dashboardStore) AdminHealthCounts(context.Context) (adminAudiobookHealth, error) {
	if d.healthErr {
		return adminAudiobookHealth{}, os.ErrInvalid
	}
	return adminAudiobookHealth{AwaitingConversion: d.awaiting}, nil
}

func (d dashboardStore) ListStoredMP3s(context.Context) ([]audiobookStoredMP3, error) {
	if d.mp3sErr {
		return nil, os.ErrInvalid
	}
	return d.mp3s, nil
}

func dashboardWith(t *testing.T, store audiobookStore) (map[string]adminSystemHealth, adminWorkQueues) {
	t.Helper()
	s, _, _ := newShareTestServer(t)
	s.config.DataDir = t.TempDir()
	s.audiobooks = store
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
	var body struct {
		Health     []adminSystemHealth `json:"system_health"`
		WorkQueues adminWorkQueues     `json:"work_queues"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	seen := map[string]adminSystemHealth{}
	for _, h := range body.Health {
		seen[h.Name] = h
	}
	return seen, body.WorkQueues
}

func TestAdminDashboardAudiobooksHealthRow(t *testing.T) {
	cases := []struct {
		name        string
		store       dashboardStore
		wantStatus  string
		wantHealthy bool
	}{
		{"success", dashboardStore{}, "Operational", true},
		{"partial activity failure", dashboardStore{healthErr: true}, "Degraded", false},
		{"stats error", dashboardStore{statsErr: true}, "Unavailable", false},
		{"activity error", dashboardStore{listeningErr: true, healthErr: true, mp3sErr: true}, "Unavailable", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			health, _ := dashboardWith(t, tc.store)
			got, ok := health["Audiobooks"]
			if !ok {
				t.Fatalf("no Audiobooks row in %+v", health)
			}
			if got.Status != tc.wantStatus || got.Healthy != tc.wantHealthy {
				t.Errorf("Audiobooks = %+v, want %s healthy=%v", got, tc.wantStatus, tc.wantHealthy)
			}
		})
	}
}

func TestAdminDashboardConversionQueueExcludesFailed(t *testing.T) {
	dir := t.TempDir()
	var mp3s []audiobookStoredMP3
	for _, name := range []string{"a.mp3", "b.mp3", "c.mp3"} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		mp3s = append(mp3s, audiobookStoredMP3{ID: name, StoragePath: p})
	}
	recordAudiobookRemuxFailure(mp3s[0].StoragePath, os.ErrInvalid)
	_, queues := dashboardWith(t, dashboardStore{mp3s: mp3s, awaiting: 3})
	if queues.ConversionQueue != 2 {
		t.Errorf("conversion_queue = %d, want 2 (3 MP3s, 1 permanently failed)", queues.ConversionQueue)
	}
}
