package dbschema

import (
	"context"
	"embed"
	"fmt"
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

func MustSQL(name string) string {
	sql, err := files.ReadFile("sql/" + name)
	if err != nil {
		panic(err)
	}
	return strings.TrimSpace(string(sql))
}
