package accessor

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewPool opens a pgx connection pool with explicit, tunable sizing. The process
// keeps two pools against the same database (the meme store and the auth user
// store), so the ceiling matters: the default pgx MaxConns of max(4, NumCPU) can
// be a surprising bottleneck once auth, browse, and asset requests all contend.
//
// Every knob is overridable from the connection URL (pool_max_conns,
// pool_min_conns, pool_max_conn_lifetime, pool_max_conn_idle_time,
// pool_health_check_period); this only fills in sane defaults when the URL is
// silent.
func NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
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

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return pool, nil
}
