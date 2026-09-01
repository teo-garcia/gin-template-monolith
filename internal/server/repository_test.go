package server_test

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/teo-garcia/gin-template-monolith/internal/modules/tasks"
)

// memoryRepository is an in-memory tasks.Repository used by the HTTP tests.
//
// It lets the whole router be exercised without Postgres, which keeps `make
// test` runnable with no infrastructure. The Postgres implementation is
// covered separately by the integration suite.
type memoryRepository struct {
	mu    sync.RWMutex
	items map[string]tasks.Task
	order []string
}

func newMemoryRepository(seed ...tasks.Task) *memoryRepository {
	repo := &memoryRepository{items: map[string]tasks.Task{}}
	for _, task := range seed {
		repo.items[task.ID] = task
		repo.order = append(repo.order, task.ID)
	}
	return repo
}

func (r *memoryRepository) List(_ context.Context, filter tasks.ListFilter) ([]tasks.Task, int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	filter.Normalize()

	matched := make([]tasks.Task, 0, len(r.items))
	for _, id := range r.order {
		task, ok := r.items[id]
		if !ok || task.DeletedAt != nil {
			continue
		}
		if filter.Status != nil && task.Status != *filter.Status {
			continue
		}
		if filter.Priority != nil && task.Priority != *filter.Priority {
			continue
		}
		matched = append(matched, task)
	}
	sort.SliceStable(matched, func(i, j int) bool {
		return matched[i].Priority > matched[j].Priority
	})

	total := len(matched)
	start := min(filter.Offset(), total)
	end := min(start+filter.PageSize, total)
	return matched[start:end], total, nil
}

func (r *memoryRepository) GetByID(_ context.Context, id string) (tasks.Task, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	task, ok := r.items[id]
	if !ok || task.DeletedAt != nil {
		return tasks.Task{}, tasks.ErrNotFound
	}
	return task, nil
}

func (r *memoryRepository) Create(_ context.Context, task tasks.Task) (tasks.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.items[task.ID] = task
	r.order = append(r.order, task.ID)
	return task, nil
}

func (r *memoryRepository) Update(_ context.Context, id string, input tasks.UpdateInput) (tasks.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	task, ok := r.items[id]
	if !ok || task.DeletedAt != nil {
		return tasks.Task{}, tasks.ErrNotFound
	}
	if input.Title != nil {
		task.Title = *input.Title
	}
	if input.Description != nil {
		task.Description = input.Description
	}
	if input.Status != nil {
		task.Status = *input.Status
	}
	if input.Priority != nil {
		task.Priority = *input.Priority
	}
	r.items[id] = task
	return task, nil
}

func (r *memoryRepository) SoftDelete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	task, ok := r.items[id]
	if !ok || task.DeletedAt != nil {
		return tasks.ErrNotFound
	}
	now := time.Now()
	task.DeletedAt = &now
	r.items[id] = task
	return nil
}
