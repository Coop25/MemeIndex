// Package workgate lets a restore hold background jobs still while it
// replaces the database and media directories underneath them.
//
// Each background job wraps one unit of work (one meme to tag, one link to
// retry, one audiobook to remux, one sweep) in Enter/Leave. Pause stops new
// units from starting and waits for the ones already running to finish;
// the returned resume function lets them start again. Jobs blocked in Enter
// give up when their stop channel closes, so a shutdown that begins during a
// restore is never stuck behind the pause.
package workgate

import (
	"context"
	"sync"
)

// Gate is the pause point shared by a set of background jobs. The zero value
// is an open gate; a nil *Gate is always open and never pauses.
type Gate struct {
	mu      sync.Mutex
	paused  int           // outstanding Pause calls not yet resumed
	active  int           // units of work between Enter and Leave
	resumed chan struct{} // closed when paused drops back to zero
	idle    chan struct{} // closed when active drops to zero during a pause
}

// Enter blocks while the gate is paused, then marks one unit of work as
// running and reports true; the caller must call Leave when the unit is
// done. It reports false, without entering, once stop has closed - whether
// that happens during a pause or before Enter is called, so no new unit
// starts after shutdown begins.
func (g *Gate) Enter(stop <-chan struct{}) bool {
	if g == nil {
		return true
	}
	for {
		select {
		case <-stop:
			return false
		default:
		}
		g.mu.Lock()
		if g.paused == 0 {
			g.active++
			g.mu.Unlock()
			return true
		}
		resumed := g.resumed
		g.mu.Unlock()
		select {
		case <-resumed:
		case <-stop:
			return false
		}
	}
}

// TryEnter is Enter without the wait: if the gate is open it marks one unit
// of work as running and reports true (the caller must call Leave); if a
// pause is in effect it reports false at once. It suits best-effort work
// that should be skipped, not delayed, while a restore runs.
func (g *Gate) TryEnter() bool {
	if g == nil {
		return true
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.paused > 0 {
		return false
	}
	g.active++
	return true
}

// Leave marks a unit of work started by Enter as finished.
func (g *Gate) Leave() {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.active--
	if g.active == 0 && g.idle != nil {
		close(g.idle)
		g.idle = nil
	}
}

// Pause stops new units of work from starting and waits until every unit
// already running has called Leave. On success it returns the function that
// lifts this pause (pauses nest; the gate opens when every one is lifted).
// If ctx ends first the pause is lifted again and ctx's error returned.
func (g *Gate) Pause(ctx context.Context) (resume func(), err error) {
	if g == nil {
		return func() {}, nil
	}
	g.mu.Lock()
	g.paused++
	if g.paused == 1 {
		g.resumed = make(chan struct{})
	}
	var idle chan struct{}
	if g.active > 0 {
		if g.idle == nil {
			g.idle = make(chan struct{})
		}
		idle = g.idle
	}
	g.mu.Unlock()

	resume = sync.OnceFunc(func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		g.paused--
		if g.paused == 0 {
			close(g.resumed)
			g.resumed = nil
		}
	})
	if idle != nil {
		select {
		case <-idle:
		case <-ctx.Done():
			resume()
			return nil, ctx.Err()
		}
	}
	return resume, nil
}

// Paused reports whether a pause is in effect.
func (g *Gate) Paused() bool {
	if g == nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.paused > 0
}
