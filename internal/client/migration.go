package client

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Server-to-server migration: instead of downloading backup parts in a
// browser and uploading them again, the destination server is given the
// source's address and a token generated on the source's Backup & Restore
// page. The destination then pulls a freshly built archive straight from the
// source over one HTTP stream and restores from it as it arrives, so neither
// side writes the archive to disk and no browser request has to stay open.
//
// Both halves are off unless enabled: MEMEINDEX_MIGRATION_SOURCE_ENABLED on
// the server being copied, MEMEINDEX_MIGRATION_IMPORT_ENABLED on the server
// being replaced.

const (
	migrationTokenTTL = 24 * time.Hour
	// migrationErrorTrailer carries the source's error when building the
	// archive fails after the stream has started and the status code is
	// already sent.
	migrationErrorTrailer = "X-Memeindex-Migration-Error"
	// migrationStallTimeout aborts a transfer that has received nothing for
	// this long - a dead connection would otherwise hang the import forever.
	migrationStallTimeout = 10 * time.Minute
	// migrationDefaultPort is assumed when the address is a bare host or IP,
	// matching the default MEMEINDEX_ADDR.
	migrationDefaultPort = "8080"
)

// --- Source side: tokens and the export stream ------------------------------

type migrationTokenStore struct {
	mu        sync.Mutex
	hash      [sha256.Size]byte
	createdAt time.Time
	expiresAt time.Time
}

type migrationTokenStatus struct {
	Active    bool       `json:"active"`
	CreatedAt *time.Time `json:"created_at,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// issue replaces any previous token with a new one. Only its hash is kept,
// so the token itself is shown once and cannot be read back later.
func (t *migrationTokenStore) issue() (string, migrationTokenStatus, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", migrationTokenStatus{}, err
	}
	token := "mig_" + base64.RawURLEncoding.EncodeToString(raw)
	t.mu.Lock()
	defer t.mu.Unlock()
	t.hash = sha256.Sum256([]byte(token))
	t.createdAt = time.Now().UTC()
	t.expiresAt = t.createdAt.Add(migrationTokenTTL)
	return token, t.statusLocked(), nil
}

func (t *migrationTokenStore) revoke() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.hash = [sha256.Size]byte{}
	t.createdAt, t.expiresAt = time.Time{}, time.Time{}
}

func (t *migrationTokenStore) status() migrationTokenStatus {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.statusLocked()
}

func (t *migrationTokenStore) statusLocked() migrationTokenStatus {
	if t.expiresAt.IsZero() || time.Now().After(t.expiresAt) {
		return migrationTokenStatus{}
	}
	createdAt, expiresAt := t.createdAt, t.expiresAt
	return migrationTokenStatus{Active: true, CreatedAt: &createdAt, ExpiresAt: &expiresAt}
}

func (t *migrationTokenStore) valid(token string) bool {
	if token == "" {
		return false
	}
	presented := sha256.Sum256([]byte(token))
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.expiresAt.IsZero() || time.Now().After(t.expiresAt) {
		return false
	}
	return subtle.ConstantTimeCompare(presented[:], t.hash[:]) == 1
}

// migrationSourceInfo is what a source reports before a transfer, so the
// destination can show what it is about to import and how big it is.
type migrationSourceInfo struct {
	AppVersion     string `json:"app_version"`
	FormatVersion  int    `json:"format_version"`
	EstimatedBytes int64  `json:"estimated_bytes"`
	Memes          int64  `json:"memes"`
	Audiobooks     int64  `json:"audiobooks"`
	Users          int64  `json:"users"`
}

func (b *portableBackup) migrationInfo(ctx context.Context) (migrationSourceInfo, error) {
	info := migrationSourceInfo{AppVersion: BuildVersion(), FormatVersion: portableBackupFormatVersion}
	pool, err := pgxpool.New(ctx, b.databaseURL)
	if err != nil {
		return info, fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()
	if err := pool.QueryRow(ctx, "SELECT (SELECT COUNT(*) FROM memes), (SELECT COUNT(*) FROM audiobooks), (SELECT COUNT(*) FROM app_users)").
		Scan(&info.Memes, &info.Audiobooks, &info.Users); err != nil {
		return info, fmt.Errorf("count library: %w", err)
	}
	// Media dominates the archive and is already compressed, so the size of
	// the media directories is a close estimate of the transfer size.
	for _, dir := range []string{"uploads", "thumbnails", "audiobooks"} {
		root := filepath.Join(b.dataDir, dir)
		_ = filepath.Walk(root, func(path string, fi os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if fi.IsDir() && dir == "audiobooks" && path == filepath.Join(root, "tmp") {
				return filepath.SkipDir
			}
			if fi.Mode().IsRegular() {
				info.EstimatedBytes += fi.Size()
			}
			return nil
		})
	}
	return info, nil
}

func bearerToken(r *http.Request) string {
	value := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(value) > 7 && strings.EqualFold(value[:7], "bearer ") {
		return strings.TrimSpace(value[7:])
	}
	return ""
}

// withMigrationToken guards the endpoints another server calls. They use the
// migration token instead of a browser session, and do not exist at all
// unless this server is configured as a migration source.
func (s *Server) withMigrationToken(next http.HandlerFunc) http.Handler {
	return s.rateLimitByIP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.config.MigrationSourceEnabled || s.backup == nil {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.migrationTokens.valid(bearerToken(r)) {
			log.Printf("migration request from %s rejected: invalid or expired token", sanitizeLogField(s.clientIP(r)))
			http.Error(w, "invalid or expired migration token", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}), s.authLimiter, "migration")
}

func (s *Server) handleMigrationInfo(w http.ResponseWriter, r *http.Request) {
	info, err := s.backup.migrationInfo(r.Context())
	if err != nil {
		log.Printf("migration info failed: %v", err)
		http.Error(w, "failed to read this server's library: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) handleMigrationExport(w http.ResponseWriter, r *http.Request) {
	log.Printf("migration export to %s started", sanitizeLogField(s.clientIP(r)))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Trailer", migrationErrorTrailer)
	w.WriteHeader(http.StatusOK)
	// Send the headers now: the archive's first bytes can take a while (the
	// database tables are dumped first) and the destination is waiting.
	_ = http.NewResponseController(w).Flush()
	if err := s.backup.writeArchive(r.Context(), w); err != nil {
		log.Printf("migration export to %s failed: %v", sanitizeLogField(s.clientIP(r)), err)
		w.Header().Set(migrationErrorTrailer, sanitizeLogField(err.Error()))
		return
	}
	// The token is a 24-hour key to a full dump of this server, users
	// included. Once it has done its one job, stop honouring it, so a copy
	// left in a chat log or shell history cannot pull the library again. A
	// failed export leaves it valid so the destination can simply retry.
	s.migrationTokens.revoke()
	log.Printf("migration export to %s completed; migration token revoked", sanitizeLogField(s.clientIP(r)))
}

// --- Destination side: pulling from a source --------------------------------

type migrationImportStatus struct {
	// State is idle, running, completed, failed or cancelled.
	State string `json:"state"`
	// Phase, while running: connecting, snapshot, transferring, restoring.
	Phase          string               `json:"phase,omitempty"`
	Source         string               `json:"source,omitempty"`
	SourceInfo     *migrationSourceInfo `json:"source_info,omitempty"`
	BytesReceived  int64                `json:"bytes_received"`
	EstimatedBytes int64                `json:"estimated_bytes"`
	StartedAt      *time.Time           `json:"started_at,omitempty"`
	CompletedAt    *time.Time           `json:"completed_at,omitempty"`
	Error          string               `json:"error,omitempty"`
}

type migrationImporter struct {
	mu       sync.Mutex
	status   migrationImportStatus
	received atomic.Int64
	cancel   context.CancelFunc
}

func (m *migrationImporter) snapshot() migrationImportStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	status := m.status
	if status.State == "" {
		status.State = "idle"
	}
	if status.SourceInfo != nil {
		info := *status.SourceInfo
		status.SourceInfo = &info
	}
	status.BytesReceived = m.received.Load()
	return status
}

func (m *migrationImporter) update(change func(*migrationImportStatus)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	change(&m.status)
}

// migrationHTTPClient talks to the source server directly. It deliberately
// does not use http.DefaultTransport, which pinDownloaderDefaultTransport
// restricts to public addresses - a migration source is usually on the LAN.
// There is no overall timeout because a transfer can take hours; a stalled
// connection is caught by migrationStallTimeout instead.
func migrationHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext:           (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			TLSHandshakeTimeout:   15 * time.Second,
			ResponseHeaderTimeout: 2 * time.Minute,
			DisableCompression:    true,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// normalizeMigrationSource turns what an admin types - "192.168.1.20",
// "nas:8080", "https://memes.example.com" - into a base URL. A bare host gets
// http:// and, without a port, the default MemeIndex port.
func normalizeMigrationSource(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("enter the source server's address")
	}
	bare := !strings.Contains(raw, "://")
	if bare {
		raw = "http://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("source must be an address like 192.168.1.20:8080 or https://memes.example.com")
	}
	if bare && parsed.Port() == "" {
		parsed.Host = net.JoinHostPort(parsed.Hostname(), migrationDefaultPort)
	}
	return &url.URL{Scheme: parsed.Scheme, Host: parsed.Host, Path: strings.TrimSuffix(parsed.Path, "/")}, nil
}

func migrationRequest(ctx context.Context, base *url.URL, path, token string) (*http.Request, error) {
	endpoint := *base
	endpoint.Path += path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "MemeIndex-Migration/"+BuildVersion())
	return req, nil
}

func describeMigrationResponse(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	message := strings.TrimSpace(string(body))
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return errors.New("the source rejected the token - it may be mistyped, expired, or replaced by a newer one")
	case resp.StatusCode == http.StatusNotFound:
		return errors.New("the source does not accept migrations - set MEMEINDEX_MIGRATION_SOURCE_ENABLED=true on it (and check the address points at MemeIndex)")
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		return fmt.Errorf("the source redirected to %s - enter that address instead", resp.Header.Get("Location"))
	case message != "":
		return fmt.Errorf("the source answered %d: %s", resp.StatusCode, message)
	default:
		return fmt.Errorf("the source answered %d", resp.StatusCode)
	}
}

// probeMigrationSource checks the address and token and reports what the
// source holds, without changing anything on either server.
func probeMigrationSource(ctx context.Context, client *http.Client, base *url.URL, token string) (migrationSourceInfo, error) {
	var info migrationSourceInfo
	req, err := migrationRequest(ctx, base, "/api/migration/info", token)
	if err != nil {
		return info, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return info, fmt.Errorf("could not reach %s: %w", base.Host, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return info, describeMigrationResponse(resp)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&info); err != nil {
		return info, errors.New("the address answered but is not a MemeIndex server")
	}
	// The same range restoreArchive accepts: an older source is fine, a newer
	// one may send data this server's schema cannot hold.
	if info.FormatVersion < portableBackupOldestFormatVersion || info.FormatVersion > portableBackupFormatVersion {
		return info, fmt.Errorf("the source (MemeIndex %s) uses backup format %d, which this server cannot import - update this server to the same or a newer version than the source", info.AppVersion, info.FormatVersion)
	}
	return info, nil
}

// progressReader counts bytes as they pass and records when the last ones
// arrived, for the progress display and the stall watchdog.
type progressReader struct {
	r        io.Reader
	count    *atomic.Int64
	lastRead *atomic.Int64
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if n > 0 {
		p.count.Add(int64(n))
		p.lastRead.Store(time.Now().UnixNano())
	}
	return n, err
}

var errMigrationRunning = errors.New("a migration is already running")

// startMigrationImport begins replacing this server with the source's
// library in the background. The caller has already probed the source.
func (s *Server) startMigrationImport(base *url.URL, token string, info migrationSourceInfo) (migrationImportStatus, error) {
	if s.backup.exportStatus().State == "running" {
		return migrationImportStatus{}, errors.New("wait for the running backup to finish before migrating")
	}
	// The same process-wide maintenance lock a restore holds: it is kept for
	// the whole job, so writes are refused from before the snapshot until the
	// restored library is live.
	if !s.restoring.CompareAndSwap(false, true) {
		return s.migration.snapshot(), errMigrationRunning
	}
	ctx, cancel := context.WithCancel(context.Background())
	startedAt := time.Now().UTC()
	s.migration.received.Store(0)
	s.migration.update(func(st *migrationImportStatus) {
		*st = migrationImportStatus{
			State:          "running",
			Phase:          "snapshot",
			Source:         base.String(),
			SourceInfo:     &info,
			EstimatedBytes: info.EstimatedBytes,
			StartedAt:      &startedAt,
		}
	})
	s.migration.mu.Lock()
	s.migration.cancel = cancel
	s.migration.mu.Unlock()

	go func() {
		defer s.restoring.Store(false)
		defer cancel()
		err := s.runMigrationImport(ctx, base, token)
		completedAt := time.Now().UTC()
		s.migration.update(func(st *migrationImportStatus) {
			st.CompletedAt = &completedAt
			st.Phase = ""
			switch {
			case err == nil:
				st.State = "completed"
			case errors.Is(ctx.Err(), context.Canceled):
				st.State = "cancelled"
				st.Error = "The migration was cancelled; this server was left unchanged."
			default:
				st.State = "failed"
				st.Error = err.Error()
			}
		})
		s.migration.mu.Lock()
		s.migration.cancel = nil
		s.migration.mu.Unlock()
		if err != nil {
			log.Printf("migration from %s failed: %v", base.Host, err)
			return
		}
		log.Printf("migration from %s completed", base.Host)
		if err := s.managers.ReloadAfterRestore(); err != nil {
			log.Printf("reload in-memory state after migration failed: %v", err)
		}
	}()
	return s.migration.snapshot(), nil
}

func (s *Server) runMigrationImport(ctx context.Context, base *url.URL, token string) error {
	// Snapshot first, while nothing is streaming yet: the source would
	// otherwise sit blocked on a full socket for as long as the snapshot
	// takes.
	s.backup.snapshotForUndo(ctx)
	if ctx.Err() != nil {
		return ctx.Err()
	}

	s.migration.update(func(st *migrationImportStatus) { st.Phase = "connecting" })
	req, err := migrationRequest(ctx, base, "/api/migration/export", token)
	if err != nil {
		return err
	}
	resp, err := migrationHTTPClient().Do(req)
	if err != nil {
		return fmt.Errorf("could not reach %s: %w", base.Host, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return describeMigrationResponse(resp)
	}

	var lastRead atomic.Int64
	lastRead.Store(time.Now().UnixNano())
	watchdogCtx, stopWatchdog := context.WithCancel(ctx)
	defer stopWatchdog()
	stalled := atomic.Bool{}
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-watchdogCtx.Done():
				return
			case <-ticker.C:
				if time.Since(time.Unix(0, lastRead.Load())) > migrationStallTimeout {
					stalled.Store(true)
					_ = resp.Body.Close()
					return
				}
			}
		}
	}()

	s.migration.update(func(st *migrationImportStatus) { st.Phase = "transferring" })
	body := &progressReader{r: resp.Body, count: &s.migration.received, lastRead: &lastRead}
	err = s.backup.restoreArchive(ctx, body, defaultMaxExtractBytes+2*s.migration.snapshot().EstimatedBytes, func() {
		stopWatchdog()
		s.migration.update(func(st *migrationImportStatus) { st.Phase = "restoring" })
	})
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if stalled.Load() {
		return fmt.Errorf("the transfer stalled: nothing arrived from the source for %s", migrationStallTimeout)
	}
	// A failure on the source after the stream started shows up here as a
	// truncated archive; the real reason travels in the trailer once the
	// body has been read to its end.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<20))
	if reason := strings.TrimSpace(resp.Trailer.Get(migrationErrorTrailer)); reason != "" {
		return fmt.Errorf("the source failed while sending its library: %s", reason)
	}
	return err
}

func (s *Server) cancelMigrationImport() bool {
	s.migration.mu.Lock()
	cancel := s.migration.cancel
	phase := s.migration.status.Phase
	s.migration.mu.Unlock()
	// Once the database load begins the restore runs to its commit (or
	// rolls back by itself on error); stopping it midway gains nothing.
	if cancel == nil || phase == "restoring" {
		return false
	}
	cancel()
	return true
}

// --- Admin endpoints ---------------------------------------------------------

type migrationOverview struct {
	Available     bool                  `json:"available"`
	SourceEnabled bool                  `json:"source_enabled"`
	ImportEnabled bool                  `json:"import_enabled"`
	Token         migrationTokenStatus  `json:"token"`
	Import        migrationImportStatus `json:"import"`
}

type migrationTargetRequest struct {
	Source string `json:"source"`
	Token  string `json:"token"`
}

func (s *Server) handleAdminMigration(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, migrationOverview{
		Available:     s.backup != nil,
		SourceEnabled: s.config.MigrationSourceEnabled,
		ImportEnabled: s.config.MigrationImportEnabled,
		Token:         s.migrationTokens.status(),
		Import:        s.migration.snapshot(),
	})
}

func (s *Server) handleAdminMigrationToken(w http.ResponseWriter, r *http.Request) {
	if !s.config.MigrationSourceEnabled || s.backup == nil {
		http.Error(w, "this server is not a migration source; set MEMEINDEX_MIGRATION_SOURCE_ENABLED=true (PostgreSQL storage required)", http.StatusConflict)
		return
	}
	switch r.Method {
	case http.MethodPost:
		token, status, err := s.migrationTokens.issue()
		if err != nil {
			http.Error(w, "failed to generate a token", http.StatusInternalServerError)
			return
		}
		log.Printf("migration token generated; valid until %s", status.ExpiresAt.Format(time.RFC3339))
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, map[string]any{"token": token, "status": status})
	case http.MethodDelete:
		s.migrationTokens.revoke()
		log.Printf("migration token revoked")
		writeJSON(w, http.StatusOK, map[string]any{"status": s.migrationTokens.status()})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) readMigrationTarget(w http.ResponseWriter, r *http.Request) (*url.URL, string, bool) {
	if !s.config.MigrationImportEnabled || s.backup == nil {
		http.Error(w, "this server cannot import from another server; set MEMEINDEX_MIGRATION_IMPORT_ENABLED=true (PostgreSQL storage required)", http.StatusConflict)
		return nil, "", false
	}
	var payload migrationTargetRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return nil, "", false
	}
	base, err := normalizeMigrationSource(payload.Source)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return nil, "", false
	}
	token := strings.TrimSpace(payload.Token)
	if token == "" {
		http.Error(w, "enter the migration token from the source server's Backup & Restore page", http.StatusBadRequest)
		return nil, "", false
	}
	return base, token, true
}

func (s *Server) handleAdminMigrationProbe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	base, token, ok := s.readMigrationTarget(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	info, err := probeMigrationSource(ctx, migrationHTTPClient(), base, token)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"source": base.String(), "info": info})
}

func (s *Server) handleAdminMigrationImport(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		base, token, ok := s.readMigrationTarget(w, r)
		if !ok {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		info, err := probeMigrationSource(ctx, migrationHTTPClient(), base, token)
		cancel()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		status, err := s.startMigrationImport(base, token, info)
		if err != nil {
			if errors.Is(err, errMigrationRunning) {
				http.Error(w, "a migration or restore is already in progress", http.StatusConflict)
				return
			}
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		log.Printf("migration from %s started", base.Host)
		writeJSON(w, http.StatusAccepted, status)
	case http.MethodDelete:
		if !s.cancelMigrationImport() {
			http.Error(w, "no migration can be cancelled right now", http.StatusConflict)
			return
		}
		writeJSON(w, http.StatusOK, s.migration.snapshot())
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
