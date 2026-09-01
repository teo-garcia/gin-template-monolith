// Package tasks is the sample domain module: handler → service → repository.
//
// It exists to demonstrate the layering, validation, pagination, soft delete,
// and error mapping every other module in a generated project should follow.
package tasks

import "time"

// Status is a task's lifecycle state.
type Status string

// The task lifecycle. These values are stored in the database and appear on the
// wire, so they are part of the API contract.
const (
	StatusPending    Status = "PENDING"
	StatusInProgress Status = "IN_PROGRESS"
	StatusCompleted  Status = "COMPLETED"
	StatusCancelled  Status = "CANCELLED"
)

// Valid reports whether s is a known status.
func (s Status) Valid() bool {
	switch s {
	case StatusPending, StatusInProgress, StatusCompleted, StatusCancelled:
		return true
	default:
		return false
	}
}

// AllStatuses lists every valid status, for validation messages and OpenAPI.
func AllStatuses() []Status {
	return []Status{StatusPending, StatusInProgress, StatusCompleted, StatusCancelled}
}

// Task is the domain entity.
type Task struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Description *string    `json:"description"`
	Status      Status     `json:"status"`
	Priority    int        `json:"priority"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	DeletedAt   *time.Time `json:"deletedAt,omitempty"`
}

// Pagination bounds. A list endpoint without a maximum page size is an
// unbounded query waiting to happen.
const (
	DefaultPage     = 1
	DefaultPageSize = 20
	MaxPageSize     = 100
	MaxPriority     = 10
)

// ListFilter describes a page of tasks to fetch.
type ListFilter struct {
	Status   *Status
	Priority *int
	Page     int
	PageSize int
}

// Normalize clamps pagination into the supported range.
//
// Out-of-range input is corrected rather than rejected: a client asking for
// page 0 or 10,000 items wants a page of results, not a 422.
func (f *ListFilter) Normalize() {
	if f.Page < 1 {
		f.Page = DefaultPage
	}
	if f.PageSize < 1 {
		f.PageSize = DefaultPageSize
	}
	if f.PageSize > MaxPageSize {
		f.PageSize = MaxPageSize
	}
}

// Offset renders the SQL offset for the requested page.
func (f ListFilter) Offset() int { return (f.Page - 1) * f.PageSize }

// CreateInput is a validated create request.
type CreateInput struct {
	Title       string  `json:"title"       binding:"required,min=1,max=200"`
	Description *string `json:"description" binding:"omitempty,max=2000"`
	Status      *Status `json:"status"      binding:"omitempty"`
	Priority    *int    `json:"priority"    binding:"omitempty,min=0,max=10"`
}

// UpdateInput is a validated partial update. Every field is optional; only the
// ones present are written.
type UpdateInput struct {
	Title       *string `json:"title"       binding:"omitempty,min=1,max=200"`
	Description *string `json:"description" binding:"omitempty,max=2000"`
	Status      *Status `json:"status"      binding:"omitempty"`
	Priority    *int    `json:"priority"    binding:"omitempty,min=0,max=10"`
}

// IsEmpty reports whether the update would change nothing.
func (u UpdateInput) IsEmpty() bool {
	return u.Title == nil && u.Description == nil && u.Status == nil && u.Priority == nil
}
