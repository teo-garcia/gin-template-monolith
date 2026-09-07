// Package database owns the GORM connection and its underlying SQL pool.
package database

import (
	"context"
	"fmt"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/teo-garcia/gin-template-monolith/internal/config"
)

// Connect builds a GORM PostgreSQL connection, configures its shared pool, and
// verifies reachability before returning.
func Connect(ctx context.Context, cfg config.Config) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(cfg.Database.URL), &gorm.Config{
		DisableAutomaticPing: true,
		// Application logging is structured; GORM's console logger would bypass
		// that boundary. Repository methods wrap errors with operation context.
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("open connection pool: %w", err)
	}
	sqlDB.SetMaxOpenConns(int(cfg.Database.PoolMax))
	// database/sql exposes retained idle capacity rather than eager minimum
	// connections. Reusing PoolMin here keeps a single pool beneath GORM.
	sqlDB.SetMaxIdleConns(int(cfg.Database.PoolMin))
	sqlDB.SetConnMaxLifetime(cfg.Database.MaxLifetime)

	pingCtx, cancel := context.WithTimeout(ctx, cfg.Database.ConnTimeout)
	defer cancel()
	if err := sqlDB.PingContext(pingCtx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return db, nil
}

// Close releases the shared SQL connection pool.
func Close(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("get connection pool: %w", err)
	}
	if err := sqlDB.Close(); err != nil {
		return fmt.Errorf("close connection pool: %w", err)
	}
	return nil
}

// Ping checks database reachability within a bounded time.
func Ping(ctx context.Context, db *gorm.DB, timeout time.Duration) error {
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("get connection pool: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return sqlDB.PingContext(pingCtx)
}
