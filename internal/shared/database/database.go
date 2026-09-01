// Package database owns the Postgres connection pool.
package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/teo-garcia/gin-template-monolith/internal/config"
)

// Connect builds a pgx pool from config and verifies it before returning.
//
// The ping matters: without it the first real request would be the one to
// discover that the database is unreachable, long after the process reported
// itself as started.
func Connect(ctx context.Context, cfg config.Config) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.Database.URL)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}

	poolCfg.MaxConns = cfg.Database.PoolMax
	poolCfg.MinConns = cfg.Database.PoolMin
	poolCfg.MaxConnLifetime = cfg.Database.MaxLifetime
	poolCfg.ConnConfig.ConnectTimeout = cfg.Database.ConnTimeout

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create connection pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, cfg.Database.ConnTimeout)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return pool, nil
}

// Stats renders pool statistics for the health payload.
func Stats(pool *pgxpool.Pool) map[string]any {
	s := pool.Stat()
	return map[string]any{
		"total":             s.TotalConns(),
		"idle":              s.IdleConns(),
		"acquired":          s.AcquiredConns(),
		"max":               s.MaxConns(),
		"acquireDuration":   s.AcquireDuration().String(),
		"emptyAcquireCount": s.EmptyAcquireCount(),
	}
}

// Ping checks database reachability within a bounded time.
func Ping(ctx context.Context, pool *pgxpool.Pool, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return pool.Ping(ctx)
}
