package dbschema

import (
	"context"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
)

// withoutStatementTimeout runs fn on a single pooled connection with
// statement_timeout disabled, so a migration that builds an index on a large
// table is not cancelled by the pool's per-statement limit (see
// accessor.NewPool). The connection's timeout is reset to its startup value
// before it goes back to the pool; if that fails the connection is closed
// rather than returned without a limit. Anything other than a pool (a test
// double, a transaction) is passed through unchanged.
func withoutStatementTimeout(ctx context.Context, db execer, fn func(execer) error) error {
	pool, ok := db.(*pgxpool.Pool)
	if !ok {
		return fn(db)
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, `SET statement_timeout = 0`); err != nil {
		return err
	}
	defer func() {
		if _, err := conn.Exec(context.WithoutCancel(ctx), `RESET statement_timeout`); err != nil {
			log.Printf("dbschema: reset statement_timeout failed, discarding connection: %v", err)
			_ = conn.Conn().Close(context.WithoutCancel(ctx))
		}
	}()
	return fn(conn)
}
