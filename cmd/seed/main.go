// Command seed loads deterministic sample data for local development and e2e
// test setup.
//
// It is deterministic on purpose: the same fixed ids and rows every run, so a
// test can assert against them and a reseed does not multiply the dataset.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/teo-garcia/gin-template-monolith/internal/config"
	"github.com/teo-garcia/gin-template-monolith/internal/shared/database"
)

// sampleTasks mirrors the seed sets in the Nest, FastAPI, Django, and Spring
// templates so a generated project looks the same whichever stack it came from.
type seedTask struct {
	ID          string
	Title       string
	Description string
	Status      string
	Priority    int
}

func (seedTask) TableName() string { return "tasks" }

var sampleTasks = []seedTask{
	{"seed-task-0001", "Set up local environment", "Copy .env.example and start the Compose stack.", "COMPLETED", 5},
	{"seed-task-0002", "Review the API contract", "Read /docs and confirm the response envelope.", "IN_PROGRESS", 4},
	{"seed-task-0003", "Add a domain module", "Copy internal/modules/tasks as the starting point.", "PENDING", 3},
	{"seed-task-0004", "Wire observability", "Point OTEL_EXPORTER_OTLP_TRACES_ENDPOINT at the collector.", "PENDING", 2},
	{"seed-task-0005", "Plan the first migration", "Follow the expand-contract rules in the README.", "PENDING", 1},
	{"seed-task-0006", "Retire the sample module", "Delete tasks once real domains exist.", "CANCELLED", 0},
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	if cfg.IsProduction() {
		return fmt.Errorf("refusing to seed a production database")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db, err := database.Connect(ctx, cfg)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer func() { _ = database.Close(db) }()

	// One transaction so a partial failure leaves no half-seeded database.
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, task := range sampleTasks {
			updates := map[string]any{
				"title":       task.Title,
				"description": task.Description,
				"status":      task.Status,
				"priority":    task.Priority,
				"deleted_at":  nil,
			}
			result := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "id"}},
				DoUpdates: clause.Assignments(updates),
			}).Create(&task)
			if result.Error != nil {
				return fmt.Errorf("seed task %s: %w", task.ID, result.Error)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("commit seed transaction: %w", err)
	}

	fmt.Printf("seeded %d tasks\n", len(sampleTasks))
	return nil
}
