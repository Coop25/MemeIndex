package accessor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgconn/ctxwatch"
	"github.com/jackc/pgx/v5/pgxpool"
)

// cancelledQueryDeadline is how long a connection whose context was
// cancelled waits for Postgres to act on the cancel request before pgx gives
// up on it and closes the connection instead.
const cancelledQueryDeadline = 5 * time.Second

// pgQueryCanceled is the SQLSTATE Postgres answers a cancel request with.
const pgQueryCanceled = "57014"

// IsQueryCanceled reports whether err is a query stopped by its context
// being cancelled. Because NewPool cancels on the server, such a query fails
// with Postgres's own query_canceled error rather than context.Canceled, so
// checking for context.Canceled alone misses it. A statement_timeout reports
// the same SQLSTATE, so callers must also check their own ctx.Err() before
// treating the error as an abandoned request.
func IsQueryCanceled(err error) bool {
	if errors.Is(err, context.Canceled) {
		return true
	}
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgQueryCanceled
}

// DefaultStatementTimeout bounds how long any single statement on a NewPool
// connection may run before Postgres cancels it. Store calls run on the
// caller's context, so a request the client abandons stops its query, but
// background jobs and the writes that must finish once started (they use
// context.WithoutCancel) are not tied to any client. Without a server-side
// limit a stuck query there (lock wait, runaway plan) would hold its
// connection forever and, repeated, drain the pool until every request hangs.
// Schema migrations lift it (see dbschema.Apply), since an index build on a
// large library can take longer.
const DefaultStatementTimeout = 30 * time.Second

// NewPool opens a pgx connection pool with explicit, tunable sizing. The process
// keeps three long-lived pools against the same database (the meme store, the
// auth user store and the audiobook store), plus short-lived ones that backup
// export, restore and migration open for the length of a job. Each pool's
// ceiling counts against Postgres's max_connections, and within a pool the
// default pgx MaxConns of max(4, NumCPU) can be a surprising bottleneck once
// auth, browse, and asset requests all contend.
//
// Every knob is overridable from the connection URL (pool_max_conns,
// pool_min_conns, pool_max_conn_lifetime, pool_max_conn_idle_time,
// pool_health_check_period, statement_timeout); this only fills in sane
// defaults when the URL is silent.
func NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := newPoolConfig(databaseURL)
	if err != nil {
		return nil, err
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return pool, nil
}

func newPoolConfig(databaseURL string) (*pgxpool.Config, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}

	if !strings.Contains(databaseURL, "pool_max_conns") && cfg.MaxConns < 10 {
		cfg.MaxConns = 10
	}
	if !strings.Contains(databaseURL, "pool_min_conns") && cfg.MinConns < 2 {
		cfg.MinConns = 2
	}
	if !strings.Contains(databaseURL, "pool_max_conn_lifetime") {
		cfg.MaxConnLifetime = 30 * time.Minute
	}
	if !strings.Contains(databaseURL, "pool_max_conn_idle_time") {
		cfg.MaxConnIdleTime = 5 * time.Minute
	}
	if !strings.Contains(databaseURL, "pool_health_check_period") {
		cfg.HealthCheckPeriod = time.Minute
	}

	// Cancel a query on the server before the call returns. pgx's default
	// closes the connection as soon as the context ends and sends Postgres
	// its cancel request only afterwards, from a background goroutine. The
	// call therefore returns while the query, if it was waiting on a lock or
	// still executing, runs on with its transaction's locks held. A caller
	// that moves straight on (the next request, a cleanup that skips locked
	// rows) then still finds those rows locked. Here the call returns only
	// once Postgres has acted on the cancel. A connection used outside a
	// transaction also goes back to the pool instead of being thrown away.
	// One inside a transaction is still discarded when its deferred Rollback
	// runs on the cancelled context. If Postgres does not answer the cancel,
	// the socket deadline is the fallback.
	cfg.ConnConfig.BuildContextWatcherHandler = func(conn *pgconn.PgConn) ctxwatch.Handler {
		return &pgconn.CancelRequestContextWatcherHandler{Conn: conn, DeadlineDelay: cancelledQueryDeadline}
	}

	// Sent as a startup parameter, so it applies to every connection the pool
	// opens with no extra round trip. A statement_timeout in the URL (a query
	// parameter, or -c statement_timeout=... in options) wins.
	if !strings.Contains(databaseURL, "statement_timeout") {
		if cfg.ConnConfig.RuntimeParams == nil {
			cfg.ConnConfig.RuntimeParams = map[string]string{}
		}
		cfg.ConnConfig.RuntimeParams["statement_timeout"] = fmt.Sprint(DefaultStatementTimeout.Milliseconds())
	}
	return cfg, nil
}
