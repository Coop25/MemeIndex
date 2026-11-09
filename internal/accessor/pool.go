package accessor

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DefaultStatementTimeout bounds how long any single statement on a NewPool
// connection may run before Postgres cancels it. Most store calls use
// context.Background(), so without a server-side limit a stuck query (lock
// wait, runaway plan) would hold its connection forever and, repeated, drain
// the pool until every request hangs. Schema migrations lift it (see
// dbschema.Apply), since an index build on a large library can take longer.
const DefaultStatementTimeout = 30 * time.Second

// NewPool opens a pgx connection pool with explicit, tunable sizing. The process
// keeps two pools against the same database (the meme store and the auth user
// store), so the ceiling matters: the default pgx MaxConns of max(4, NumCPU) can
// be a surprising bottleneck once auth, browse, and asset requests all contend.
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
