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

// StopBackgroundWork stops the link retry queue and waits for detached link
// imports, cancelling them if they outlast half of timeout. Call it after the
// HTTP servers have shut down (so no new imports start) and before closing
// the store.
func (s *Server) StopBackgroundWork(timeout time.Duration) {
	if !s.background.Stop(timeout) {
		log.Printf("background link work still running after %s; closing anyway", timeout)
	}
}
