package dbschema

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

//go:embed sql/*.sql
var files embed.FS

type execer interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

// querier is execer plus the single-row read applyTracked needs to consult the
// schema_migrations ledger. *pgxpool.Pool and pgx.Tx both satisfy it.
type querier interface {
	execer
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Apply runs the named migration files in order, exactly once each. Applied
// files are recorded in a schema_migrations ledger keyed by file name, so
// steady-state startups do no schema work at all. The SQL files are still
// individually idempotent (IF NOT EXISTS ...), which keeps two instances
// booting at once safe: at worst they both run the same no-op DDL before one of
// them wins the ledger insert.
func Apply(ctx context.Context, db execer, names ...string) error {
	q, ok := db.(querier)
	if !ok {
		// Callers in this codebase always pass a *pgxpool.Pool; this branch only
		// exists so a bare execer (e.g. a test double) still gets the old
		// run-every-time behaviour instead of a panic.
		return applyUntracked(ctx, db, names...)
	}
	return applyTracked(ctx, q, names...)
}

func applyUntracked(ctx context.Context, db execer, names ...string) error {
	for _, name := range names {
		sql, err := files.ReadFile("sql/" + name)
		if err != nil {
			return fmt.Errorf("read schema file %s: %w", name, err)
		}
		if _, err := db.Exec(ctx, string(sql)); err != nil {
			return fmt.Errorf("apply schema file %s: %w", name, err)
		}
	}
	return nil
}

const createMigrationsLedger = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    name       TEXT PRIMARY KEY,
    checksum   TEXT NOT NULL,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
)`

func applyTracked(ctx context.Context, db querier, names ...string) error {
	if _, err := db.Exec(ctx, createMigrationsLedger); err != nil {
		return fmt.Errorf("ensure schema_migrations ledger: %w", err)
	}

	for _, name := range names {
		body, err := files.ReadFile("sql/" + name)
		if err != nil {
			return fmt.Errorf("read schema file %s: %w", name, err)
		}
		sum := checksum(body)

		var recorded string
		switch err := db.QueryRow(ctx, `SELECT checksum FROM schema_migrations WHERE name = $1`, name).Scan(&recorded); {
		case err == nil:
			if recorded != sum {
				// The file changed after it was applied. Do not silently re-run
				// it - that is how partial/destructive edits corrupt a database.
				log.Printf("dbschema: %s already applied with a different checksum (recorded %s, file %s); leaving the applied version in place", name, short(recorded), short(sum))
			}
			continue
		case errors.Is(err, pgx.ErrNoRows):
			// Not applied yet - fall through and apply it.
		default:
			return fmt.Errorf("check schema_migrations for %s: %w", name, err)
		}

		if _, err := db.Exec(ctx, string(body)); err != nil {
			return fmt.Errorf("apply schema file %s: %w", name, err)
		}
		if _, err := db.Exec(ctx,
			`INSERT INTO schema_migrations (name, checksum) VALUES ($1, $2) ON CONFLICT (name) DO NOTHING`,
			name, sum,
		); err != nil {
			return fmt.Errorf("record schema migration %s: %w", name, err)
		}
		log.Printf("dbschema: applied %s", name)
	}
	return nil
}

func checksum(body []byte) string {
	h := sha256.Sum256(body)
	return hex.EncodeToString(h[:])
}

func short(sum string) string {
	if len(sum) <= 12 {
		return sum
	}
	return sum[:12]
}

// ApplyOptional runs migrations that improve performance but are not required
// for correctness (for example, indexes that depend on a contrib extension the
// database role may not be allowed to create). It is deliberately NOT tracked
// in schema_migrations: a failure is logged and swallowed so startup still
// succeeds on a locked-down database, and it is retried on the next boot in
// case the missing capability has since been enabled. It returns the names that
// did not apply, so the caller can surface a degraded-capability signal (e.g.
// on /readyz and the admin dashboard).
func ApplyOptional(ctx context.Context, db execer, names ...string) (notApplied []string) {
	for _, name := range names {
		sql, err := files.ReadFile("sql/" + name)
		if err != nil {
			log.Printf("dbschema: optional migration %s unreadable: %v", name, err)
			notApplied = append(notApplied, name)
			continue
		}
		if _, err := db.Exec(ctx, string(sql)); err != nil {
			log.Printf("dbschema: optional migration %s not applied (continuing without it): %v", name, err)
			notApplied = append(notApplied, name)
		}
	}
	return notApplied
}

func MustSQL(name string) string {
	sql, err := files.ReadFile("sql/" + name)
	if err != nil {
		panic(err)
	}
	return strings.TrimSpace(string(sql))
}
