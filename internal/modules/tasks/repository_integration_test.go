//go:build integration

package tasks_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/teo-garcia/gin-template-monolith/internal/modules/tasks"
)

func TestPostgresRepositoryLifecycle(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("DATABASE_URL is required for integration tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to Postgres: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping Postgres: %v", err)
	}

	if _, err := pool.Exec(ctx, "TRUNCATE TABLE tasks"); err != nil {
		t.Fatalf("reset tasks table: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, cleanupErr := pool.Exec(cleanupCtx, "TRUNCATE TABLE tasks"); cleanupErr != nil {
			t.Errorf("clean tasks table: %v", cleanupErr)
		}
	})

	repository := tasks.NewPostgresRepository(pool)
	description := "stored by pgx"

	low, err := repository.Create(ctx, tasks.Task{
		ID:       "integration-low",
		Title:    "Low priority",
		Status:   tasks.StatusPending,
		Priority: 2,
	})
	if err != nil {
		t.Fatalf("create low-priority task: %v", err)
	}
	high, err := repository.Create(ctx, tasks.Task{
		ID:          "integration-high",
		Title:       "High priority",
		Description: &description,
		Status:      tasks.StatusInProgress,
		Priority:    8,
	})
	if err != nil {
		t.Fatalf("create high-priority task: %v", err)
	}
	if low.CreatedAt.IsZero() || high.UpdatedAt.IsZero() {
		t.Fatal("database timestamps were not populated")
	}

	minimumPriority := 5
	status := tasks.StatusInProgress
	items, total, err := repository.List(ctx, tasks.ListFilter{
		Status:   &status,
		Priority: &minimumPriority,
		Page:     1,
		PageSize: 10,
	})
	if err != nil {
		t.Fatalf("list filtered tasks: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].ID != high.ID {
		t.Fatalf("filtered list = (%v, %d), want only %s", items, total, high.ID)
	}

	title := "Updated without replacing omitted fields"
	completed := tasks.StatusCompleted
	updated, err := repository.Update(ctx, high.ID, tasks.UpdateInput{
		Title:  &title,
		Status: &completed,
	})
	if err != nil {
		t.Fatalf("update task: %v", err)
	}
	if updated.Title != title || updated.Status != completed || updated.Priority != high.Priority {
		t.Fatalf("partial update replaced persisted fields: %+v", updated)
	}
	if updated.Description == nil || *updated.Description != description {
		t.Fatalf("partial update lost description: %+v", updated.Description)
	}

	if err := repository.SoftDelete(ctx, high.ID); err != nil {
		t.Fatalf("soft delete task: %v", err)
	}
	if _, err := repository.GetByID(ctx, high.ID); !errors.Is(err, tasks.ErrNotFound) {
		t.Fatalf("get soft-deleted task error = %v, want ErrNotFound", err)
	}
	if err := repository.SoftDelete(ctx, high.ID); !errors.Is(err, tasks.ErrNotFound) {
		t.Fatalf("repeat soft delete error = %v, want ErrNotFound", err)
	}

	items, total, err = repository.List(ctx, tasks.ListFilter{Page: 2, PageSize: 10})
	if err != nil {
		t.Fatalf("list empty page: %v", err)
	}
	if len(items) != 0 || total != 1 {
		t.Fatalf("empty page = (%v, %d), want no rows and total 1", items, total)
	}
}
