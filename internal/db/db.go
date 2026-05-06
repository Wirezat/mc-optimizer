// Package db wraps pgx/v5 for direct SQL access.
package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DB wraps pgxpool.Pool.
type DB struct{ Pool *pgxpool.Pool }

// New opens and pings a connection pool from dsn; caller should defer Close.
func New(ctx context.Context, dsn string) (*DB, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("db: open pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db: ping: %w", err)
	}
	return &DB{Pool: pool}, nil
}

// Close releases all pool connections.
func (d *DB) Close() { d.Pool.Close() }
