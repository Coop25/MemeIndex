package workgate

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPauseWaitsForWorkInFlight(t *testing.T) {
	var g Gate
	if !g.Enter(nil) {
		t.Fatal("Enter on an open gate failed")
	}

	paused := make(chan func(), 1)
	go func() {
		resume, err := g.Pause(context.Background())
		if err != nil {
			t.Errorf("Pause: %v", err)
		}
		paused <- resume
	}()
	select {
	case <-paused:
		t.Fatal("Pause returned while a unit of work was still running")
	case <-time.After(50 * time.Millisecond):
	}

	g.Leave()
	var resume func()
	select {
	case resume = <-paused:
	case <-time.After(5 * time.Second):
		t.Fatal("Pause never returned after the work finished")
	}

	entered := make(chan bool, 1)
	go func() { entered <- g.Enter(nil) }()
	select {
	case <-entered:
		t.Fatal("Enter succeeded while paused")
	case <-time.After(50 * time.Millisecond):
	}
	resume()
	select {
	case ok := <-entered:
		if !ok {
			t.Fatal("Enter failed after resume")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Enter still blocked after resume")
	}
	g.Leave()
	resume() // a second call is a no-op
	if g.Paused() {
		t.Fatal("gate still paused after resume")
	}
}

func TestEnterGivesUpWhenStopCloses(t *testing.T) {
	var g Gate
	resume, err := g.Pause(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer resume()
	stop := make(chan struct{})
	entered := make(chan bool, 1)
	go func() { entered <- g.Enter(stop) }()
	close(stop)
	select {
	case ok := <-entered:
		if ok {
			t.Fatal("Enter reported success after stop closed during a pause")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Enter ignored stop while paused")
	}
}

func TestPauseGivesUpWithContextAndReopens(t *testing.T) {
	var g Gate
	g.Enter(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := g.Pause(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Pause with a stuck unit: err = %v, want deadline exceeded", err)
	}
	if g.Paused() {
		t.Fatal("a failed Pause left the gate paused")
	}
	g.Leave()
}

func TestNestedPauses(t *testing.T) {
	var g Gate
	first, _ := g.Pause(context.Background())
	second, _ := g.Pause(context.Background())
	first()
	if !g.Paused() {
		t.Fatal("gate opened while a second pause was still held")
	}
	second()
	if g.Paused() {
		t.Fatal("gate still paused after every pause was lifted")
	}
}

func TestTryEnterSkipsInsteadOfWaiting(t *testing.T) {
	var g Gate
	if !g.TryEnter() {
		t.Fatal("TryEnter on an open gate failed")
	}

	// A unit entered with TryEnter is waited for like any other.
	paused := make(chan func(), 1)
	go func() {
		resume, _ := g.Pause(context.Background())
		paused <- resume
	}()
	select {
	case <-paused:
		t.Fatal("Pause returned while a TryEnter unit was still running")
	case <-time.After(50 * time.Millisecond):
	}
	g.Leave()
	resume := <-paused

	if g.TryEnter() {
		t.Fatal("TryEnter succeeded while paused")
	}
	resume()
	if !g.TryEnter() {
		t.Fatal("TryEnter failed after resume")
	}
	g.Leave()
}

func TestNilGateIsAlwaysOpen(t *testing.T) {
	var g *Gate
	if !g.Enter(nil) {
		t.Fatal("nil gate refused Enter")
	}
	g.Leave()
	resume, err := g.Pause(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	resume()
}
