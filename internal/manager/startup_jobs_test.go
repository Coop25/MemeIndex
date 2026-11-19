package manager

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"memeindex/internal/accessor/accessortest"
)

// previewBackfillStore stands in for the Postgres store's thumbnail backfill:
// each pass runs one step per item and records what it rendered. A render
// listed in block waits there until that channel closes.
type previewBackfillStore struct {
	*accessortest.MemeStore
	items []string

	mu       sync.Mutex
	passes   int
	rendered []string
	block    map[string]chan struct{}
	entered  chan string
}

func (s *previewBackfillStore) EnsurePreviewAssets(_ context.Context, step func(work func()) bool) error {
	s.mu.Lock()
	s.passes++
	s.mu.Unlock()
	if step == nil {
		step = func(work func()) bool {
			work()
			return true
		}
	}
	for _, item := range s.items {
		if !step(func() { s.render(item) }) {
			return nil
		}
	}
	return nil
}

func (s *previewBackfillStore) render(item string) {
	s.mu.Lock()
	wait := s.block[item]
	delete(s.block, item) // only the first render of an item blocks
	s.mu.Unlock()
	if wait != nil {
		s.entered <- item
		<-wait
	}
	s.mu.Lock()
	s.rendered = append(s.rendered, item)
	s.mu.Unlock()
}

func (s *previewBackfillStore) progress() (passes int, rendered []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.passes, append([]string(nil), s.rendered...)
}

func newPreviewBackfillManager(t *testing.T, items ...string) (*MemeManager, *previewBackfillStore) {
	t.Helper()
	memes, err := accessortest.NewMemeStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewMemeStore: %v", err)
	}
	store := &previewBackfillStore{MemeStore: memes, items: items, block: map[string]chan struct{}{}, entered: make(chan string, 1)}
	return NewMemeManager(store), store
}

func waitForRendered(t *testing.T, store *previewBackfillStore, want []string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, rendered := store.progress()
		if slices.Equal(rendered, want) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("rendered = %v, want %v", rendered, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// The preview backfill used to run on a bare goroutine from main, rendering
// thumbnails while a restore swapped the thumbnails directory. Now it waits
// a restore out, without even listing the memes until it is over.
func TestPreviewBackfillWaitsOutRestore(t *testing.T) {
	m, store := newPreviewBackfillManager(t, "a", "b")
	defer m.StopBackgroundWorkers(5 * time.Second)
	resume, err := m.PauseBackgroundWork(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	m.StartPreviewAssetBackfill()
	time.Sleep(100 * time.Millisecond)
	if passes, rendered := store.progress(); passes != 0 || len(rendered) != 0 {
		t.Fatalf("during a restore: passes=%d rendered=%v, want nothing", passes, rendered)
	}

	resume()
	waitForRendered(t, store, []string{"a", "b"})
}

// A restore waits for the thumbnail being rendered, holds the rest back, and
// once it has replaced the library the backfill starts over against it
// instead of carrying on with the memes it listed before.
func TestPreviewBackfillRestartsAfterRestore(t *testing.T) {
	m, store := newPreviewBackfillManager(t, "a", "b")
	defer m.StopBackgroundWorkers(5 * time.Second)
	releaseA := make(chan struct{})
	store.block["a"] = releaseA

	m.StartPreviewAssetBackfill()
	select {
	case <-store.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the backfill never started rendering")
	}

	paused := make(chan struct{})
	var resume func()
	go func() {
		var err error
		resume, err = m.PauseBackgroundWork(context.Background())
		if err != nil {
			t.Errorf("PauseBackgroundWork: %v", err)
		}
		close(paused)
	}()
	assertBlocked(t, paused, "PauseBackgroundWork")
	close(releaseA)
	waitDone(t, paused, "PauseBackgroundWork")

	time.Sleep(100 * time.Millisecond)
	if _, rendered := store.progress(); !slices.Equal(rendered, []string{"a"}) {
		t.Fatalf("rendered during the restore = %v, want only the one in flight", rendered)
	}
	if err := m.ReloadAfterRestore(); err != nil {
		t.Fatalf("ReloadAfterRestore: %v", err)
	}
	resume()

	waitForRendered(t, store, []string{"a", "a", "b"})
	if passes, _ := store.progress(); passes != 2 {
		t.Fatalf("backfill passes = %d, want 2 (one restarted after the restore)", passes)
	}
}

// Shutdown waits for the thumbnail being rendered and stops the backfill
// there; one that is waiting out a restore gives up at once.
func TestStopBackgroundWorkersStopsPreviewBackfill(t *testing.T) {
	m, store := newPreviewBackfillManager(t, "a", "b")
	release := make(chan struct{})
	store.block["a"] = release
	m.StartPreviewAssetBackfill()
	<-store.entered

	stopped := make(chan struct{})
	go func() {
		m.StopBackgroundWorkers(5 * time.Second)
		close(stopped)
	}()
	assertBlocked(t, stopped, "StopBackgroundWorkers with a thumbnail in flight")
	close(release)
	waitDone(t, stopped, "StopBackgroundWorkers")
	if _, rendered := store.progress(); !slices.Equal(rendered, []string{"a"}) {
		t.Fatalf("rendered = %v, want the backfill to stop after the one in flight", rendered)
	}

	paused, _ := newPreviewBackfillManager(t, "a")
	resume, err := paused.PauseBackgroundWork(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer resume()
	paused.StartPreviewAssetBackfill()
	start := time.Now()
	paused.StopBackgroundWorkers(5 * time.Second)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("stopping a backfill paused by a restore took %s", elapsed)
	}
}

// The tag-scratch sweep deletes files from the thumbnails directory a restore
// swaps, so it waits a restore out too.
func writeTagScratch(t *testing.T, m *MemeManager) string {
	t.Helper()
	scratch := filepath.Join(m.ThumbnailDir(), "tagframes", "a-tag-1.jpg")
	if err := os.MkdirAll(filepath.Dir(scratch), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scratch, []byte("frame"), 0o600); err != nil {
		t.Fatal(err)
	}
	return scratch
}

func TestTagScratchSweepWaitsOutRestore(t *testing.T) {
	m := newLifecycleManager(t)
	defer m.StopBackgroundWorkers(5 * time.Second)
	scratch := writeTagScratch(t, m)
	resume, err := m.PauseBackgroundWork(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	m.StartTagSuggestionWorkerAfterSweep()
	time.Sleep(100 * time.Millisecond)
	if _, err := os.Stat(scratch); err != nil {
		t.Fatalf("the sweep ran during a restore: %v", err)
	}

	resume()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(scratch); os.IsNotExist(err) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the sweep never ran after the restore")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A sweep waiting out a restore is worker-tracked work: stopping the workers
// ends it at once, without sweeping, and it never goes on to start the
// suggestion worker.
func TestStopBackgroundWorkersDuringPausedSweep(t *testing.T) {
	m := newLifecycleManager(t)
	scratch := writeTagScratch(t, m)
	resume, err := m.PauseBackgroundWork(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer resume()
	m.StartTagSuggestionWorkerAfterSweep()

	start := time.Now()
	m.StopBackgroundWorkers(5 * time.Second)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("stopping the workers during a paused sweep took %s", elapsed)
	}
	time.Sleep(50 * time.Millisecond)
	if _, err := os.Stat(scratch); err != nil {
		t.Fatalf("the sweep ran after the workers stopped: %v", err)
	}
}
