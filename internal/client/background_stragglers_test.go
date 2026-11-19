package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// profileTouchStore records the auth profile touches that reach the user
// store. With release set, each touch blocks there until release closes (or
// its context ends), standing in for a slow database.
type profileTouchStore struct {
	authUserStore
	calls   atomic.Int32
	entered chan struct{}
	release chan struct{}
}

func (p *profileTouchStore) UpsertSessionProfile(ctx context.Context, _ authClaims) error {
	p.calls.Add(1)
	if p.release == nil {
		return nil
	}
	close(p.entered)
	select {
	case <-p.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// newProfileTouchServer wires an auth service to a Server the way NewServer
// does, so profile touches run as the Server's background jobs.
func newProfileTouchServer(store authUserStore) *Server {
	s := &Server{auth: newRevocationAuthService(store)}
	s.auth.runBestEffort = s.startBestEffortJob
	return s
}

func profileTouchThrottled(a *authService, userID string) bool {
	a.sessionMu.Lock()
	defer a.sessionMu.Unlock()
	_, ok := a.lastProfileWrite[userID]
	return ok
}

// The profile touch runs on every authenticated request, reads included,
// which maintenanceGuard lets through during a restore. It used to upsert
// app_users from an untracked goroutine while the restore was truncating
// it. Now it is skipped (never waited for) and retried by the next request.
func TestProfileTouchSkippedDuringRestore(t *testing.T) {
	store := &profileTouchStore{}
	s := newProfileTouchServer(store)
	defer s.StopBackgroundWork(5 * time.Second)
	resume, err := s.pauseBackgroundWork(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	s.auth.maybeTouchProfile(authClaims{Subject: "alice"})
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("profile touch held the request for %s during a restore", elapsed)
	}
	time.Sleep(100 * time.Millisecond)
	if calls := store.calls.Load(); calls != 0 {
		t.Fatalf("profile touch reached app_users %d time(s) during a restore", calls)
	}
	if profileTouchThrottled(s.auth, "alice") {
		t.Fatal("a skipped touch left the throttle set, so the next request would not retry it")
	}

	resume()
	s.auth.maybeTouchProfile(authClaims{Subject: "alice"})
	deadline := time.Now().Add(5 * time.Second)
	for store.calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if calls := store.calls.Load(); calls != 1 {
		t.Fatalf("profile touches after the restore = %d, want 1", calls)
	}
}

// A touch already writing when a restore starts is waited for, and shutdown
// waits for it before main closes the user store's pool.
func TestProfileTouchInFlightIsWaitedFor(t *testing.T) {
	store := &profileTouchStore{entered: make(chan struct{}), release: make(chan struct{})}
	s := newProfileTouchServer(store)
	s.auth.maybeTouchProfile(authClaims{Subject: "alice"})
	select {
	case <-store.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("profile touch never started")
	}

	paused := make(chan func(), 1)
	go func() {
		resume, err := s.pauseBackgroundWork(context.Background())
		if err != nil {
			t.Errorf("pauseBackgroundWork: %v", err)
		}
		paused <- resume
	}()
	select {
	case <-paused:
		t.Fatal("a restore started while a profile touch was still writing")
	case <-time.After(100 * time.Millisecond):
	}

	var released atomic.Bool
	go func() {
		time.Sleep(100 * time.Millisecond)
		released.Store(true)
		close(store.release)
	}()
	select {
	case resume := <-paused:
		resume()
	case <-time.After(5 * time.Second):
		t.Fatal("the restore never started after the profile touch finished")
	}

	// Shutdown waits for a touch in flight too.
	store.entered, store.release = make(chan struct{}), make(chan struct{})
	released.Store(false)
	s.auth.invalidateAll() // clear the throttle so this touch writes
	s.auth.maybeTouchProfile(authClaims{Subject: "alice"})
	<-store.entered
	go func() {
		time.Sleep(100 * time.Millisecond)
		released.Store(true)
		close(store.release)
	}()
	s.StopBackgroundWork(10 * time.Second)
	if !released.Load() {
		t.Fatal("StopBackgroundWork returned while a profile touch was still writing")
	}
}

// A migration import used to run on an untracked goroutine: shutdown neither
// waited for it nor stopped it, so main closed the stores underneath it and
// the process exited with the maintenance flag still set mid-transfer. Now
// shutdown cancels it and waits for it to unwind.
func TestShutdownCancelsMigrationImport(t *testing.T) {
	release := make(chan struct{})
	requested := make(chan struct{}, 1)
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("\x1f\x8b")) // the start of a gzip stream, then nothing
		w.(http.Flusher).Flush()
		requested <- struct{}{}
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer source.Close()
	defer close(release)

	dataDir := t.TempDir()
	s := &Server{backup: &portableBackup{
		dataDir:    dataDir,
		exportFunc: func(context.Context) (string, error) { return "", errors.New("no database in this test") },
	}}
	base, _ := url.Parse(source.URL)
	if _, err := s.startMigrationImport(base, "mig_x", migrationSourceInfo{}); err != nil {
		t.Fatalf("startMigrationImport: %v", err)
	}
	select {
	case <-requested:
	case <-time.After(5 * time.Second):
		t.Fatal("the import never reached the source")
	}
	if !s.restoring.Load() || !s.jobs.Paused() {
		t.Fatal("a running import should hold the maintenance lock and the pause")
	}

	start := time.Now()
	s.StopBackgroundWork(10 * time.Second)
	// Well inside the grace period: shutdown cancels the import at once
	// rather than waiting for it to give up on its own.
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("shutdown took %s with a migration import running", elapsed)
	}
	if s.restoring.Load() {
		t.Fatal("shutdown left the maintenance flag set")
	}
	if s.jobs.Paused() {
		t.Fatal("shutdown left background work paused")
	}
	if status := s.migration.snapshot(); status.State != "cancelled" {
		t.Fatalf("import state after shutdown = %q (%s), want cancelled", status.State, status.Error)
	}
	leftovers, _ := filepath.Glob(filepath.Join(dataDir, ".memeindex-import-*"))
	if len(leftovers) != 0 {
		t.Fatalf("a cancelled import must clean up its staging directory, found %v", leftovers)
	}
}

// Once shutdown has begun, a new import is refused and leaves nothing set.
func TestMigrationImportRefusedAfterShutdown(t *testing.T) {
	s := &Server{backup: &portableBackup{dataDir: t.TempDir()}}
	s.StopBackgroundWork(time.Second)
	base, _ := url.Parse("http://source.invalid")
	if _, err := s.startMigrationImport(base, "mig_x", migrationSourceInfo{}); err == nil {
		t.Fatal("an import started after shutdown began")
	}
	if s.restoring.Load() {
		t.Fatal("a refused import left the maintenance flag set")
	}
	if status := s.migration.snapshot(); status.State != "idle" {
		t.Fatalf("import state after a refused start = %q, want idle", status.State)
	}
}
