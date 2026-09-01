package tasks

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/teo-garcia/gin-template-monolith/internal/shared/httpx"
)

// Service holds the task business rules.
//
// It is the only layer that turns persistence failures into API errors, which
// is what keeps repository details out of handlers and handler concerns out of
// SQL.
type Service struct {
	repo Repository
}

// NewService builds a task service.
func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// List returns a page of tasks and the total count.
func (s *Service) List(ctx context.Context, filter ListFilter) ([]Task, int, error) {
	filter.Normalize()

	if filter.Status != nil && !filter.Status.Valid() {
		return nil, 0, httpx.NewValidationError("Invalid query parameters", map[string]string{
			"status": "must be one of " + statusList(),
		})
	}
	if filter.Priority != nil && (*filter.Priority < 0 || *filter.Priority > MaxPriority) {
		return nil, 0, httpx.NewValidationError("Invalid query parameters", map[string]string{
			"priority": fmt.Sprintf("must be between 0 and %d", MaxPriority),
		})
	}

	items, total, err := s.repo.List(ctx, filter)
	if err != nil {
		return nil, 0, httpx.NewDatabaseError("Could not list tasks").WithCause(err)
	}
	return items, total, nil
}

// Get returns a single task.
func (s *Service) Get(ctx context.Context, id string) (Task, error) {
	if strings.TrimSpace(id) == "" {
		return Task{}, httpx.NewBadRequestError("Task id is required")
	}

	task, err := s.repo.GetByID(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return Task{}, httpx.NewNotFoundError("Task " + id + " not found")
	}
	if err != nil {
		return Task{}, httpx.NewDatabaseError("Could not load task").WithCause(err)
	}
	return task, nil
}

// Create stores a new task, applying defaults for omitted fields.
func (s *Service) Create(ctx context.Context, input CreateInput) (Task, error) {
	title := strings.TrimSpace(input.Title)
	if title == "" {
		return Task{}, httpx.NewValidationError("Validation failed", map[string]string{
			"title": "must not be blank",
		})
	}

	status := StatusPending
	if input.Status != nil {
		if !input.Status.Valid() {
			return Task{}, httpx.NewValidationError("Validation failed", map[string]string{
				"status": "must be one of " + statusList(),
			})
		}
		status = *input.Status
	}

	priority := 0
	if input.Priority != nil {
		priority = *input.Priority
	}

	task := Task{
		ID:          uuid.NewString(),
		Title:       title,
		Description: trimOptional(input.Description),
		Status:      status,
		Priority:    priority,
	}

	created, err := s.repo.Create(ctx, task)
	if err != nil {
		return Task{}, httpx.NewDatabaseError("Could not create task").WithCause(err)
	}
	return created, nil
}

// Update applies a partial update to an existing task.
func (s *Service) Update(ctx context.Context, id string, input UpdateInput) (Task, error) {
	if input.IsEmpty() {
		return Task{}, httpx.NewValidationError("Validation failed", map[string]string{
			"body": "provide at least one field to update",
		})
	}
	if input.Status != nil && !input.Status.Valid() {
		return Task{}, httpx.NewValidationError("Validation failed", map[string]string{
			"status": "must be one of " + statusList(),
		})
	}
	if input.Title != nil {
		trimmed := strings.TrimSpace(*input.Title)
		if trimmed == "" {
			return Task{}, httpx.NewValidationError("Validation failed", map[string]string{
				"title": "must not be blank",
			})
		}
		input.Title = &trimmed
	}
	if input.Description != nil {
		input.Description = trimOptional(input.Description)
	}

	updated, err := s.repo.Update(ctx, id, input)
	if errors.Is(err, ErrNotFound) {
		return Task{}, httpx.NewNotFoundError("Task " + id + " not found")
	}
	if err != nil {
		return Task{}, httpx.NewDatabaseError("Could not update task").WithCause(err)
	}
	return updated, nil
}

// Delete soft-deletes a task.
func (s *Service) Delete(ctx context.Context, id string) error {
	err := s.repo.SoftDelete(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return httpx.NewNotFoundError("Task " + id + " not found")
	}
	if err != nil {
		return httpx.NewDatabaseError("Could not delete task").WithCause(err)
	}
	return nil
}

func statusList() string {
	all := AllStatuses()
	names := make([]string, len(all))
	for i, s := range all {
		names[i] = string(s)
	}
	return strings.Join(names, ", ")
}

// trimOptional trims an optional string, collapsing an all-whitespace value to
// nil so the column stores NULL rather than a meaningless empty string.
func trimOptional(s *string) *string {
	if s == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*s)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
