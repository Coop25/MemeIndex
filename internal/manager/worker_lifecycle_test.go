package manager

import (
	"testing"
	"time"

	"memeindex/internal/accessor"
)

func newLifecycleManager(t *testing.T) *MemeManager {
	t.Helper()
	store, err := accessor.NewMemeStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewMemeStore: %v", err)
	}
	return NewMemeManager(store)
}

func TestStopBackgroundWorkersStopsHygieneWorker(t *testing.T) {
	m := newLifecycleManager(t)
	m.StartTagHygieneWorker()
	time.Sleep(50 * time.Millisecond) // let the worker prime its snapshot and park

	done := make(chan struct{})
	go func() {
		m.StopBackgroundWorkers(2 * time.Second)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("StopBackgroundWorkers did not return; hygiene worker never exited")
	}

	// Second call is a no-op and must not panic or block.
	m.StopBackgroundWorkers(time.Second)
}

func TestDequeueTagSuggestionUnblocksOnStop(t *testing.T) {
	m := newLifecycleManager(t)

	got := make(chan bool, 1)
	go func() {
		_, ok := m.dequeueTagSuggestion()
		got <- ok
	}()
	time.Sleep(50 * time.Millisecond) // ensure the goroutine is parked in cond.Wait()

	m.StopBackgroundWorkers(2 * time.Second)

	select {
	case ok := <-got:
		if ok {
			t.Fatal("dequeueTagSuggestion returned ok=true after shutdown")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("dequeueTagSuggestion stayed blocked after StopBackgroundWorkers")
	}
}

func TestWaitOrStopReturnsFalseAfterStop(t *testing.T) {
	m := newLifecycleManager(t)
	if !m.waitOrStop(time.Millisecond) {
		t.Fatal("waitOrStop should return true when not stopping")
	}
	m.StopBackgroundWorkers(time.Second)
	if m.waitOrStop(time.Second) {
		t.Fatal("waitOrStop should return false once stopping")
	}
}
