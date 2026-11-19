package manager

import (
	"context"
	"testing"
	"time"
)

func waitDone(t *testing.T, done <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s never finished", what)
	}
}

func assertBlocked(t *testing.T, done <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-done:
		t.Fatalf("%s ran while background work was paused", what)
	case <-time.After(100 * time.Millisecond):
	}
}

// A restore's pause waits for the unit of work in flight, then holds the
// next one back until it is resumed.
func TestPauseBackgroundWorkWaitsForInFlightWork(t *testing.T) {
	m := newLifecycleManager(t)
	if !m.enterBackgroundWork() {
		t.Fatal("enterBackgroundWork failed on a running manager")
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
	m.leaveBackgroundWork()
	waitDone(t, paused, "PauseBackgroundWork")

	refreshed := make(chan struct{})
	go func() {
		m.refreshTagHygieneSnapshot()
		close(refreshed)
	}()
	assertBlocked(t, refreshed, "the tag-hygiene recompute")
	m.adminReadMu.Lock()
	cached := m.tagHygieneCache
	m.adminReadMu.Unlock()
	if cached != nil {
		t.Fatal("tag-hygiene snapshot was computed during a restore")
	}

	resume()
	waitDone(t, refreshed, "the tag-hygiene recompute")
	m.adminReadMu.Lock()
	cached = m.tagHygieneCache
	m.adminReadMu.Unlock()
	if cached == nil {
		t.Fatal("tag-hygiene snapshot was not computed after resume")
	}
}

// A meme the suggestion worker dequeued as a restore began is handed back to
// the queue (which the restore re-seeds) instead of being tagged, and the
// worker sits out the pause.
func TestTagSuggestionWorkerHandsBackMemeDuringRestore(t *testing.T) {
	m := newLifecycleManager(t)
	resume, err := m.PauseBackgroundWork(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	var retried, stopping bool
	go func() {
		retried, stopping = m.processQueuedTagSuggestion("meme-1")
		close(done)
	}()
	assertBlocked(t, done, "tagging a meme")
	m.suggestionQueueMu.Lock()
	queue := append([]string(nil), m.suggestionQueue...)
	state := m.suggestionWorkerState
	m.suggestionQueueMu.Unlock()
	if len(queue) != 1 || queue[0] != "meme-1" {
		t.Fatalf("queue during restore = %v, want the dequeued meme handed back", queue)
	}
	if state == "processing" {
		t.Fatal("worker started processing during a restore")
	}

	resume()
	waitDone(t, done, "the paused suggestion")
	if retried || stopping {
		t.Fatalf("after resume: retried=%v stopping=%v, want neither", retried, stopping)
	}
}

// Stopping the workers during a restore must not wait for the restore.
func TestStopBackgroundWorkersDuringPause(t *testing.T) {
	m := newLifecycleManager(t)
	resume, err := m.PauseBackgroundWork(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer resume()

	done := make(chan struct{})
	var cleanupErr error
	go func() {
		cleanupErr = m.CleanupStaleReelSessions()
		close(done)
	}()
	assertBlocked(t, done, "reel-session cleanup")

	m.StopBackgroundWorkers(time.Second)
	waitDone(t, done, "reel-session cleanup after stop")
	if cleanupErr != nil {
		t.Fatalf("cleanup cut short by shutdown returned %v", cleanupErr)
	}

	stopping := make(chan struct{})
	go func() {
		_, stop := m.processQueuedTagSuggestion("meme-2")
		if !stop {
			t.Error("processQueuedTagSuggestion did not report stopping")
		}
		close(stopping)
	}()
	waitDone(t, stopping, "a paused suggestion after stop")
}
