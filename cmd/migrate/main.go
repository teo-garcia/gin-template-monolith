// Command migrate applies database migrations.
//
// It is a separate binary from the API on purpose: migrations run as a
// pre-deploy step, before the new application version starts. They must never
// run from app startup, a request handler, a seed script, or test setup.
//
//	migrate up       # apply pending migrations (idempotent)
//	migrate down     # roll everything back (local/test only)
//	migrate version  # print the current schema version
package main

import (
	"fmt"
	"os"

	"github.com/teo-garcia/gin-template-monolith/internal/config"
	"github.com/teo-garcia/gin-template-monolith/internal/shared/database"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	command := "up"
	if len(args) > 0 {
		command = args[0]
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}

	migrator, err := database.NewMigrator(cfg.Database.URL)
	if err != nil {
		return err
	}
	defer func() {
		if err := migrator.Close(); err != nil {
			fmt.Fprintln(os.Stderr, "migrate: closing migrator:", err)
		}
	}()

	version, dirty, err := migrator.Version()
	if err != nil {
		return err
	}
	// A dirty schema means a previous migration failed part-way. Continuing
	// would compound an unknown state, so stop and demand a human.
	if dirty && command != "version" {
		return fmt.Errorf(
			"schema version %d is dirty; resolve it manually before running migrations", version)
	}

	switch command {
	case "up":
		if err := migrator.Up(); err != nil {
			return err
		}
		newVersion, _, err := migrator.Version()
		if err != nil {
			return err
		}
		if newVersion == version {
			fmt.Printf("no pending migrations (schema version %d)\n", version)
		} else {
			fmt.Printf("migrated schema %d -> %d\n", version, newVersion)
		}
		return nil

	case "down":
		if cfg.IsProduction() {
			return fmt.Errorf("refusing to run `down` in production; " +
				"roll back with a backup restore or a forward-fix migration")
		}
		if err := migrator.Down(); err != nil {
			return err
		}
		fmt.Println("rolled back all migrations")
		return nil

	case "version":
		fmt.Printf("schema version %d (dirty: %t)\n", version, dirty)
		return nil

	default:
		return fmt.Errorf("unknown command %q; want up, down, or version", command)
	}
}
