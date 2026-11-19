package accessor

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestNewPoolConfigSetsStatementTimeout(t *testing.T) {
	cfg, err := newPoolConfig("postgres://user:pw@localhost:5432/memeindex?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.ConnConfig.RuntimeParams["statement_timeout"]; got != "30000" {
		t.Fatalf("statement_timeout = %q, want %q", got, "30000")
	}
}

func TestNewPoolConfigKeepsStatementTimeoutFromURL(t *testing.T) {
	cfg, err := newPoolConfig("postgres://user:pw@localhost:5432/memeindex?sslmode=disable&statement_timeout=120000")
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.ConnConfig.RuntimeParams["statement_timeout"]; got != "120000" {
		t.Fatalf("statement_timeout = %q, want the URL's %q", got, "120000")
	}
}

// TestNewPoolStatementTimeoutIntegration checks the timeout reaches the server.
// It is skipped unless MEMEINDEX_TEST_DATABASE_URL points at a Postgres.
func TestNewPoolStatementTimeoutIntegration(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("MEMEINDEX_TEST_DATABASE_URL"))
	if dsn == "" || strings.Contains(dsn, "statement_timeout") {
		t.Skip("set MEMEINDEX_TEST_DATABASE_URL (without statement_timeout) to run")
	}
	ctx := context.Background()
	pool, err := NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer pool.Close()

	var got string
	if err := pool.QueryRow(ctx, `SHOW statement_timeout`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != "30s" {
		t.Fatalf("server statement_timeout = %q, want 30s", got)
	}
}

// A query whose context is cancelled must stop on the server before the call
// returns, not only on the client. pgx's default just closes the socket, and
// Postgres notices that only some time later: until then the transaction is
// still alive and still holds every lock it took, so a caller that rolls back
// and moves on (a cleanup, the next request) finds those rows still locked.
// It is skipped unless MEMEINDEX_TEST_DATABASE_URL points at a Postgres.
func TestNewPoolCancelledQueryReleasesItsLocksBeforeReturning(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("MEMEINDEX_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("set MEMEINDEX_TEST_DATABASE_URL to run")
	}
	ctx := context.Background()
	pool, err := NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer pool.Close()

	// Unique advisory lock keys so parallel runs never meet.
	var held, contended int64
	if err := pool.QueryRow(ctx, `SELECT (random() * 1e15)::bigint, (random() * 1e15)::bigint`).Scan(&held, &contended); err != nil {
		t.Fatal(err)
	}
	blocker, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Release()
	if _, err := blocker.Exec(ctx, `SELECT pg_advisory_lock($1)`, contended); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = blocker.Exec(ctx, `SELECT pg_advisory_unlock($1)`, contended) }()

	// The cancelled caller: a transaction that holds one lock, then waits on
	// another, the shape of a merge or edit blocked behind someone else.
	queryCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	tx, err := pool.Begin(queryCtx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(queryCtx, `SELECT pg_advisory_xact_lock($1)`, held); err != nil {
		t.Fatal(err)
	}
	pid := tx.Conn().PgConn().PID()
	done := make(chan error, 1)
	go func() {
		_, err := tx.Exec(queryCtx, `SELECT pg_advisory_xact_lock($1)`, contended)
		done <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var waiting bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE pid = $1 AND wait_event_type = 'Lock')`, pid).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the transaction never blocked on the contended lock")
		}
		time.Sleep(20 * time.Millisecond)
	}

	cancel()
	if err := <-done; err == nil {
		t.Fatal("the cancelled query succeeded")
	}
	_ = tx.Rollback(context.WithoutCancel(queryCtx))

	// Checked once, straight away: the caller has returned and rolled back,
	// so the lock its transaction held must already be free.
	var free bool
	if err := pool.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, held).Scan(&free); err != nil {
		t.Fatal(err)
	}
	if !free {
		t.Fatal("the cancelled transaction still holds its lock on the server after the call returned")
	}
	_, _ = pool.Exec(ctx, `SELECT pg_advisory_unlock($1)`, held)
}
