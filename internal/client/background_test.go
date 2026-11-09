package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"memeindex/internal/accessor"
)

func TestBackgroundTasksStopWaitsForInFlightWork(t *testing.T) {
	var b backgroundTasks
	release := make(chan struct{})
	var finished atomic.Bool
	b.Go(func(context.Context) {
		<-release
		finished.Store(true)
	})

	go func() {
		time.Sleep(50 * time.Millisecond)
		close(release)
	}()
	if !b.Stop(5 * time.Second) {
		t.Fatal("Stop reported unfinished work")
	}
	if !finished.Load() {
		t.Fatal("Stop returned before in-flight work finished")
	}
	if b.Go(func(context.Context) { t.Error("work started after Stop") }) {
		t.Fatal("Go accepted work after Stop")
	}
}

func TestBackgroundTasksStopCancelsWorkPastGracePeriod(t *testing.T) {
	var b backgroundTasks
	var cancelled atomic.Bool
	b.Go(func(ctx context.Context) {
		<-ctx.Done()
		cancelled.Store(true)
	})

	start := time.Now()
	if !b.Stop(200 * time.Millisecond) {
		t.Fatal("Stop reported unfinished work after cancelling it")
	}
	if !cancelled.Load() {
		t.Fatal("work was not cancelled")
	}
	if elapsed := time.Since(start); elapsed < 100*time.Millisecond {
		t.Fatalf("Stop cancelled after %s, before the grace period", elapsed)
	}
}

func TestBackgroundTasksStopGivesUpOnStuckWork(t *testing.T) {
	var b backgroundTasks
	stuck := make(chan struct{})
	defer close(stuck)
	b.Go(func(context.Context) { <-stuck })
	if b.Stop(50 * time.Millisecond) {
		t.Fatal("Stop reported success while work was still running")
	}
}

func TestLinkRetryQueueStopsAndWaitsForRunningJob(t *testing.T) {
	original := linkRetryPollInterval
	linkRetryPollInterval = 10 * time.Millisecond
	t.Cleanup(func() { linkRetryPollInterval = original })

	started := make(chan struct{})
	release := make(chan struct{})
	var finished atomic.Bool
	q := newLinkRetryQueue(time.Millisecond, 3, func(context.Context, LinkRetryJob) error {
		close(started)
		<-release
		finished.Store(true)
		return nil
	})
	q.Enqueue("https://example.com/video", nil, "", accessor.AuditActor{})
	// Make the job ready immediately.
	q.mu.Lock()
	q.queued[0].NextAttemptAt = time.Now().Add(-time.Second)
	q.mu.Unlock()

	var b backgroundTasks
	b.Go(func(ctx context.Context) { q.run(ctx, b.Quit()) })

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("retry job never started")
	}

	go func() {
		time.Sleep(50 * time.Millisecond)
		close(release)
	}()
	if !b.Stop(5 * time.Second) {
		t.Fatal("retry queue did not stop")
	}
	if !finished.Load() {
		t.Fatal("shutdown did not wait for the running retry job")
	}
}

func TestLinkRetryQueueCapsRejectedJobs(t *testing.T) {
	q := newLinkRetryQueue(time.Hour, 1, func(context.Context, LinkRetryJob) error { return nil })
	for i := range maxRejectedLinkRetries + 25 {
		q.finish(LinkRetryJob{ID: string(rune('a' + i%26)), MaxAttempts: 1}, errors.New("download failed"))
	}
	if got := len(q.Snapshot().Rejected); got != maxRejectedLinkRetries {
		t.Fatalf("rejected jobs = %d, want cap %d", got, maxRejectedLinkRetries)
	}
}

func TestImportLinkRefusedAfterShutdownBegins(t *testing.T) {
	s := &Server{}
	s.StopBackgroundWork(time.Second)

	req := httptest.NewRequest(http.MethodPost, "/api/memes/import", nil)
	_, err, pending := s.importLinkWithinBudget(req, accessor.AuditActor{}, "https://example.com/a.png", nil, "")
	if err == nil || pending {
		t.Fatalf("import after shutdown: err=%v pending=%v, want a refusal", err, pending)
	}
}
