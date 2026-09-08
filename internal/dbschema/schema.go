package dbschema

import (
	"context"
	"embed"
	"fmt"
	"log"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

//go:embed sql/*.sql
var files embed.FS

type execer interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

func Apply(ctx context.Context, db execer, names ...string) error {
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

// ApplyOptional runs migrations that improve performance but are not required
// for correctness (for example, indexes that depend on a contrib extension the
// database role may not be allowed to create). A failure is logged and
// swallowed so startup still succeeds on a locked-down database.
func ApplyOptional(ctx context.Context, db execer, names ...string) {
	for _, name := range names {
		sql, err := files.ReadFile("sql/" + name)
		if err != nil {
			log.Printf("dbschema: optional migration %s unreadable: %v", name, err)
			continue
		}
		if _, err := db.Exec(ctx, string(sql)); err != nil {
			log.Printf("dbschema: optional migration %s not applied (continuing without it): %v", name, err)
		}
	}
}

func MustSQL(name string) string {
	sql, err := files.ReadFile("sql/" + name)
	if err != nil {
		panic(err)
	}
	return strings.TrimSpace(string(sql))
}
