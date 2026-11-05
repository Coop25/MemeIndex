package client

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"memeindex/internal/dbschema"
)

// newIsolatedAudiobookStore returns a store bound to a private, throwaway
// Postgres schema, so global aggregates (counts, byte sums) are not polluted
// by rows other tests leave in the shared test database. Skips unless
// MEMEINDEX_TEST_DATABASE_URL is set.
func newIsolatedAudiobookStore(t *testing.T) (*postgresAudiobookStore, string) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("MEMEINDEX_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("set MEMEINDEX_TEST_DATABASE_URL to run the Postgres integration test")
	}
	ctx := context.Background()
	schema := "adminstats_" + strings.ReplaceAll(uuid.NewString(), "-", "")

	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		admin.Close()
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		admin.Close()
	})

	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	q := u.Query()
	q.Set("search_path", schema+",public")
	u.RawQuery = q.Encode()
	scoped := u.String()

	pool, err := pgxpool.New(ctx, scoped)
	if err != nil {
		t.Fatalf("connect scoped pool: %v", err)
	}
	err = dbschema.Apply(ctx, pool, "001_memes_core.sql")
	pool.Close()
	if err != nil {
		t.Fatalf("apply meme schema: %v", err)
	}
	dataDir := t.TempDir()
	st, err := newAudiobookStore(ctx, scoped, dataDir)
	if err != nil {
		t.Fatalf("newAudiobookStore: %v", err)
	}
	store := st.(*postgresAudiobookStore)
	t.Cleanup(store.pool.Close)
	return store, dataDir
}
