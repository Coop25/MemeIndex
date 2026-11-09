package accessor

import (
	"context"
	"os"
	"strings"
	"testing"
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
