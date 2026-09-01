package database

import (
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	// Registers the "postgres" migration driver with golang-migrate.
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	// Registers the "pgx" database/sql driver so golang-migrate can open a
	// connection with the same DATABASE_URL the pgx pool uses.
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/teo-garcia/gin-template-monolith/migrations"
)

// Migrator applies schema migrations.
type Migrator struct {
	m *migrate.Migrate
}

// NewMigrator opens a migrator against the given database URL.
func NewMigrator(databaseURL string) (*Migrator, error) {
	source, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return nil, fmt.Errorf("load embedded migrations: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", source, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open migrator: %w", err)
	}
	return &Migrator{m: m}, nil
}

// Up applies every pending migration.
//
// Running with nothing pending is a success, not an error: `db-deploy` must be
// idempotent so a redeploy of an unchanged schema does not fail the pipeline.
func (m *Migrator) Up() error {
	if err := m.m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}

// Down rolls back every migration. Local and test use only.
func (m *Migrator) Down() error {
	if err := m.m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("roll back migrations: %w", err)
	}
	return nil
}

// Version reports the current schema version and whether it is dirty.
//
// A dirty version means a migration failed part-way and the schema is in an
// unknown state; it must be resolved by hand before anything else runs.
func (m *Migrator) Version() (version uint, dirty bool, err error) {
	version, dirty, err = m.m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("read schema version: %w", err)
	}
	return version, dirty, nil
}

// Close releases the migrator's connections.
func (m *Migrator) Close() error {
	sourceErr, dbErr := m.m.Close()
	return errors.Join(sourceErr, dbErr)
}
