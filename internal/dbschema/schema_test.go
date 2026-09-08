package dbschema

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// fakeDB is an in-memory stand-in for a pgx pool that records the migration SQL
// it is asked to execute and keeps a schema_migrations ledger.
type fakeDB struct {
	ledger        map[string]string // name -> checksum
	ranFiles      []string          // migration bodies executed (by detected file marker)
	forceMismatch string
}

func newFakeDB() *fakeDB { return &fakeDB{ledger: map[string]string{}} }

func (f *fakeDB) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	switch {
	case strings.Contains(sql, "CREATE TABLE IF NOT EXISTS schema_migrations"):
		// ledger bootstrap - nothing to record
	case strings.HasPrefix(strings.TrimSpace(sql), "INSERT INTO schema_migrations"):
		name, _ := args[0].(string)
		sum, _ := args[1].(string)
		f.ledger[name] = sum
	default:
		// A migration body. Tag it by the first embedded file name we can find.
		f.ranFiles = append(f.ranFiles, firstKnownMigrationMarker(sql))
	}
	return pgconn.CommandTag{}, nil
}

func (f *fakeDB) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	name, _ := args[0].(string)
	sum, ok := f.ledger[name]
	if f.forceMismatch != "" && name == f.forceMismatch {
		return scanRow{value: "deadbeef", found: true}
	}
	return scanRow{value: sum, found: ok}
}

type scanRow struct {
	value string
	found bool
}

func (r scanRow) Scan(dest ...any) error {
	if !r.found {
		return pgx.ErrNoRows
	}
	*(dest[0].(*string)) = r.value
	return nil
}

// firstKnownMigrationMarker returns a short identifier for a migration body so
// tests can assert which files ran without embedding their full contents.
func firstKnownMigrationMarker(sql string) string {
	head := sql
	if len(head) > 40 {
		head = head[:40]
	}
	return strings.TrimSpace(strings.ReplaceAll(head, "\n", " "))
}

func TestApplyTrackedRunsEachMigrationOnce(t *testing.T) {
	db := newFakeDB()
	names := []string{"001_memes_core.sql", "009_meme_shares.sql"}

	if err := Apply(context.Background(), db, names...); err != nil {
		t.Fatalf("first Apply: %v", err)
	}
	if len(db.ranFiles) != 2 {
		t.Fatalf("first Apply ran %d migration bodies, want 2: %v", len(db.ranFiles), db.ranFiles)
	}
	if len(db.ledger) != 2 {
		t.Fatalf("ledger has %d rows after first Apply, want 2", len(db.ledger))
	}

	// Second run: ledger is populated, so no migration body should execute.
	db.ranFiles = nil
	if err := Apply(context.Background(), db, names...); err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	if len(db.ranFiles) != 0 {
		t.Fatalf("second Apply re-ran migrations: %v", db.ranFiles)
	}
}

func TestApplyTrackedSkipsChecksumMismatchWithoutRerunning(t *testing.T) {
	db := newFakeDB()
	db.forceMismatch = "001_memes_core.sql" // ledger reports a different checksum

	if err := Apply(context.Background(), db, "001_memes_core.sql"); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(db.ranFiles) != 0 {
		t.Fatalf("a checksum mismatch must not re-run the migration; ran: %v", db.ranFiles)
	}
}

func TestApplyUntrackedFallbackForBareExecer(t *testing.T) {
	// A type that only implements execer (no QueryRow) keeps the old behaviour.
	be := &bareExecer{}
	if err := Apply(context.Background(), be, "001_memes_core.sql", "001_memes_core.sql"); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if be.calls != 2 {
		t.Fatalf("bare execer path ran %d times, want 2 (no dedupe without a ledger)", be.calls)
	}
}

type bareExecer struct{ calls int }

func (b *bareExecer) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	b.calls++
	return pgconn.CommandTag{}, nil
}
