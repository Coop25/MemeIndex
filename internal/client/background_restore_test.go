package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"memeindex/internal/accessor"
	"memeindex/internal/audiobooks"
)

// remuxBlockingStore holds a remux at its first store call until released
// (or its context is cancelled), standing in for a long conversion.
type remuxBlockingStore struct {
	audiobooks.Store
	entered chan struct{}
	release chan struct{}
}

func (s *remuxBlockingStore) GetAudiobook(ctx context.Context, _ string) (audiobooks.Record, error) {
	close(s.entered)
	select {
	case <-s.release:
		return audiobooks.Record{}, audiobooks.ErrNotFound
	case <-ctx.Done():
		return audiobooks.Record{}, ctx.Err()
	}
}

func uploadedMP3(t *testing.T) audiobooks.Record {
	t.Helper()
	path := filepath.Join(t.TempDir(), "book.mp3")
	if err := os.WriteFile(path, []byte("ID3 not really audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	return audiobooks.Record{ID: "book", StoragePath: path, MimeType: "audio/mpeg"}
}

// The post-upload remux used to be a bare goroutine, so main could close the
// store underneath it. Shutdown must now wait for it.
func TestShutdownWaitsForUploadRemux(t *testing.T) {
	store := &remuxBlockingStore{entered: make(chan struct{}), release: make(chan struct{})}
	s := &Server{audiobooks: store, audiobookRemuxProgress: newAudiobookRemuxProgress()}

	s.remuxUploadedAudiobookMP3(uploadedMP3(t))
	select {
	case <-store.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("remux never started")
	}

	var released atomic.Bool
	go func() {
		time.Sleep(100 * time.Millisecond)
		released.Store(true)
		close(store.release)
	}()
	s.StopBackgroundWork(10 * time.Second)
	if !released.Load() {
		t.Fatal("StopBackgroundWork returned while the remux was still running")
	}
}

// A remux that shutdown cancels must leave neither a partial .m4b nor a
// failure marker that would keep the book from being retried for a week.
func TestCancelledRemuxLeavesNoOutputOrFailureMarker(t *testing.T) {
	store := &remuxBlockingStore{entered: make(chan struct{}), release: make(chan struct{})}
	s := &Server{audiobooks: store, audiobookRemuxProgress: newAudiobookRemuxProgress()}
	record := uploadedMP3(t)

	s.remuxUploadedAudiobookMP3(record)
	<-store.entered
	// Never released: the grace period runs out and the remux is cancelled.
	s.StopBackgroundWork(100 * time.Millisecond)

	if _, err := os.Stat(remuxedM4BPath(record.StoragePath)); !os.IsNotExist(err) {
		t.Fatalf("cancelled remux left %s behind (err %v)", remuxedM4BPath(record.StoragePath), err)
	}
	if _, err := os.Stat(audiobookRemuxFailurePath(record.StoragePath)); !os.IsNotExist(err) {
		t.Fatal("a remux cancelled by shutdown was recorded as a failed conversion")
	}
}

// staleUploadCountingStore counts the sweeper's store calls.
type staleUploadCountingStore struct {
	audiobooks.Store
	calls atomic.Int32
}

func (s *staleUploadCountingStore) StaleUploads(context.Context, time.Time) ([]audiobooks.Upload, error) {
	s.calls.Add(1)
	return nil, nil
}

func TestSweeperWaitsOutRestorePause(t *testing.T) {
	store := &staleUploadCountingStore{}
	s := &Server{audiobooks: store}
	resume, err := s.jobs.Pause(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		s.sweepStaleAudiobookUploads(context.Background())
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("sweep ran during a restore")
	case <-time.After(100 * time.Millisecond):
	}
	if store.calls.Load() != 0 {
		t.Fatal("sweeper touched the store while paused")
	}

	resume()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("sweep never ran after the restore finished")
	}
	if store.calls.Load() != 1 {
		t.Fatalf("store calls after resume = %d, want 1", store.calls.Load())
	}
}

func TestLinkRetryQueueTakesNoJobWhilePaused(t *testing.T) {
	original := linkRetryPollInterval
	linkRetryPollInterval = 10 * time.Millisecond
	t.Cleanup(func() { linkRetryPollInterval = original })

	var processed atomic.Int32
	q := newLinkRetryQueue(time.Millisecond, 3, func(context.Context, LinkRetryJob) error {
		processed.Add(1)
		return nil
	})
	s := &Server{}
	q.gate = &s.jobs
	q.Enqueue("https://example.com/video", nil, "", accessor.AuditActor{})
	q.mu.Lock()
	q.queued[0].NextAttemptAt = time.Now().Add(-time.Second)
	q.mu.Unlock()

	resume, err := s.jobs.Pause(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	s.background.Go(func(ctx context.Context) { q.run(ctx, s.background.Quit()) })
	time.Sleep(100 * time.Millisecond)
	if processed.Load() != 0 {
		t.Fatal("retry queue processed a job during a restore")
	}

	resume()
	deadline := time.Now().Add(5 * time.Second)
	for processed.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if processed.Load() != 1 {
		t.Fatalf("jobs processed after resume = %d, want 1", processed.Load())
	}
	s.StopBackgroundWork(5 * time.Second)
}

// Shutting down in the middle of a restore must not wait on the pause:
// jobs blocked waiting for it give up as soon as shutdown begins.
func TestShutdownDuringRestorePauseDoesNotHang(t *testing.T) {
	s := &Server{audiobooks: &staleUploadCountingStore{}, config: Config{AudiobookBookmarkExpiry: time.Hour}}
	resume, err := s.pauseBackgroundWork(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer resume()
	s.startAudiobookBookmarkExpirySweeper() // blocks at once, waiting on the pause
	s.startAudiobookUploadCleanupSweeper()

	start := time.Now()
	s.StopBackgroundWork(10 * time.Second)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("shutdown took %s while a restore held background work", elapsed)
	}
}

// runBackupRestore holds every background job, the meme manager's workers
// included, for the length of the restore and lets them go afterwards.
func TestBackupRestorePausesBackgroundWork(t *testing.T) {
	s, _, _ := newShareTestServer(t)
	s.backup = &portableBackup{dataDir: t.TempDir(), job: backupJobStatus{State: "idle"}}

	// A unit of work in flight when the restore starts is waited for.
	if !s.enterBackgroundJob() {
		t.Fatal("enterBackgroundJob failed")
	}
	var workFinished atomic.Bool
	go func() {
		time.Sleep(100 * time.Millisecond)
		workFinished.Store(true)
		s.leaveBackgroundJob()
	}()

	var sawPaused, sawWorkFinished bool
	r := httptest.NewRequest(http.MethodPost, "https://example.com/api/admin/backup/import", strings.NewReader(""))
	w := httptest.NewRecorder()
	s.runBackupRestore(w, r, func(context.Context) error {
		sawWorkFinished = workFinished.Load()
		sawPaused = s.jobs.Paused()
		return nil
	})
	if w.Code != http.StatusOK {
		t.Fatalf("restore: %d %s", w.Code, w.Body.String())
	}
	if !sawWorkFinished {
		t.Fatal("restore started before in-flight background work finished")
	}
	if !sawPaused {
		t.Fatal("background jobs were not paused during the restore")
	}
	if s.jobs.Paused() {
		t.Fatal("background jobs still paused after the restore")
	}
	if !s.enterBackgroundJob() {
		t.Fatal("background work could not resume after the restore")
	}
	s.leaveBackgroundJob()
}

type closeRecorder struct{ closed bool }

func (c *closeRecorder) Close() { c.closed = true }

type closingUserStore struct {
	authUserStore
	closeRecorder
}

type closingAudiobookStore struct {
	audiobooks.Store
	closeRecorder
}

// Shutdown used to close only the meme store's pool; the auth user and
// audiobook stores each hold their own.
func TestServerCloseReleasesItsStores(t *testing.T) {
	users, books := &closingUserStore{}, &closingAudiobookStore{}
	s := &Server{users: users, audiobooks: books}
	s.Close()
	if !users.closed || !books.closed {
		t.Fatalf("Close released users=%v audiobooks=%v, want both", users.closed, books.closed)
	}
	(&Server{}).Close() // no stores configured: nothing to close, no panic
}
