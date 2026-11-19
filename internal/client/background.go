package client

import (
	"context"
	"log"
	"sync"
	"time"
)

// backgroundTasks tracks goroutines the Server starts outside a request (link
// imports that outlive their response, the link retry queue) so shutdown can
// wait for them before the store they write to is closed. The zero value is
// ready to use.
type backgroundTasks struct {
	once   sync.Once
	ctx    context.Context
	cancel context.CancelFunc
	quit   chan struct{}

	mu       sync.Mutex
	stopping bool
	wg       sync.WaitGroup
}

func (b *backgroundTasks) init() {
	b.once.Do(func() {
		b.ctx, b.cancel = context.WithCancel(context.Background())
		b.quit = make(chan struct{})
	})
}

// Go runs fn in a tracked goroutine. fn's context is cancelled if shutdown's
// grace period runs out, and Quit is closed as soon as shutdown begins. It
// reports false, without running fn, once shutdown has begun.
func (b *backgroundTasks) Go(fn func(ctx context.Context)) bool {
	b.init()
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.stopping {
		return false
	}
	b.wg.Go(func() { fn(b.ctx) })
	return true
}

// Context is cancelled when shutdown gives up waiting on in-flight work.
func (b *backgroundTasks) Context() context.Context {
	b.init()
	return b.ctx
}

// Quit is closed when shutdown begins; long-running loops should stop picking
// up new work when it is.
func (b *backgroundTasks) Quit() <-chan struct{} {
	b.init()
	return b.quit
}

// Stop refuses new work, lets in-flight work finish for up to half of timeout,
// then cancels it and waits out the rest. It reports whether every tracked
// goroutine exited in time.
func (b *backgroundTasks) Stop(timeout time.Duration) bool {
	b.init()
	b.mu.Lock()
	if !b.stopping {
		b.stopping = true
		close(b.quit)
	}
	b.mu.Unlock()

	done := make(chan struct{})
	go func() {
		b.wg.Wait()
		close(done)
	}()

	grace := time.NewTimer(timeout / 2)
	defer grace.Stop()
	select {
	case <-done:
		b.cancel()
		return true
	case <-grace.C:
	}

	b.cancel()
	hard := time.NewTimer(timeout - timeout/2)
	defer hard.Stop()
	select {
	case <-done:
		return true
	case <-hard.C:
		return false
	}
}

// StopBackgroundWork stops the link retry queue, the audiobook sweepers and
// the remux backfill, cancels a migration import in progress, and waits for
// those, detached link imports, remuxes and profile touches, cancelling them
// if they outlast half of timeout. Call it after the HTTP
// servers have shut down (so no new work starts) and before Close.
func (s *Server) StopBackgroundWork(timeout time.Duration) {
	if !s.background.Stop(timeout) {
		log.Printf("background work still running after %s; closing anyway", timeout)
	}
}

// stopping reports whether shutdown has begun.
func (s *Server) stopping() bool {
	select {
	case <-s.background.Quit():
		return true
	default:
		return false
	}
}

// enterBackgroundJob waits out a restore in progress before one unit of
// background work. It reports false if shutdown begins instead (a restore
// never holds shutdown up this way); on true the caller must call
// leaveBackgroundJob when the unit is done.
func (s *Server) enterBackgroundJob() bool {
	return s.jobs.Enter(s.background.Quit())
}

func (s *Server) leaveBackgroundJob() {
	s.jobs.Leave()
}

// startBestEffortJob runs work as one tracked unit of background work, unless
// a restore is holding background work or shutdown has begun: then it skips
// work and reports false at once, without waiting. A restore that starts
// while work runs waits for it like any other unit. It is for best-effort
// writes that the next request simply retries, such as the auth profile
// touch, which runs on every authenticated request and must never hold one
// up.
func (s *Server) startBestEffortJob(work func(ctx context.Context)) bool {
	if !s.jobs.TryEnter() {
		return false
	}
	started := s.background.Go(func(ctx context.Context) {
		defer s.leaveBackgroundJob()
		work(ctx)
	})
	if !started {
		s.leaveBackgroundJob()
	}
	return started
}

// pauseBackgroundWork holds every background job still for a restore or
// migration import: this server's own (link imports and retries, remuxes, the
// metadata backfill, the sweepers) and the meme manager's workers. It waits
// for the work each has in flight to finish, so nothing is writing when the
// restore starts replacing tables and media directories. If ctx ends first
// it lifts the pause again and returns ctx's error. The returned resume lets
// everything run again; call it once the restored state has been reloaded.
func (s *Server) pauseBackgroundWork(ctx context.Context) (resume func(), err error) {
	resumeJobs, err := s.jobs.Pause(ctx)
	if err != nil {
		return nil, err
	}
	if s.managers == nil {
		return resumeJobs, nil
	}
	resumeWorkers, err := s.managers.PauseBackgroundWork(ctx)
	if err != nil {
		resumeJobs()
		return nil, err
	}
	return func() {
		resumeWorkers()
		resumeJobs()
	}, nil
}

// Close releases the database connections the Server opened itself: the auth
// user store's pool and the audiobook store's pool. The meme store belongs to
// main, which closes it separately. Call it after StopBackgroundWork, once
// nothing is left to use them.
func (s *Server) Close() {
	if closer, ok := s.users.(interface{ Close() }); ok {
		closer.Close()
	}
	if closer, ok := s.audiobooks.(interface{ Close() }); ok {
		closer.Close()
	}
}
