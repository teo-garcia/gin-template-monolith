package tasks

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned when a task does not exist or is soft-deleted.
var ErrNotFound = errors.New("task not found")

// Repository is the persistence boundary for tasks.
//
// It is an interface so the service can be unit-tested without a database, and
// so a generated project can swap the implementation without touching the
// service layer.
type Repository interface {
	List(ctx context.Context, filter ListFilter) (items []Task, total int, err error)
	GetByID(ctx context.Context, id string) (Task, error)
	Create(ctx context.Context, task Task) (Task, error)
	Update(ctx context.Context, id string, input UpdateInput) (Task, error)
	SoftDelete(ctx context.Context, id string) error
}

// PostgresRepository is the pgx-backed Repository.
//
// All SQL lives here. Query text never leaks into the service or the handler:
// that is what keeps the domain layer testable and the database swappable.
type PostgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository builds a repository over a pgx pool.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

const taskColumns = `id, title, description, status, priority, created_at, updated_at, deleted_at`

// List returns one page of tasks plus the total matching count.
//
// The count is computed in the same statement as the page via a window
// function, so pagination costs one round trip rather than two and cannot see a
// torn read between the count and the page.
func (r *PostgresRepository) List(ctx context.Context, filter ListFilter) ([]Task, int, error) {
	filter.Normalize()

	conditions := []string{"deleted_at IS NULL"}
	args := []any{}

	if filter.Status != nil {
		args = append(args, string(*filter.Status))
		conditions = append(conditions, fmt.Sprintf("status = $%d", len(args)))
	}
	if filter.Priority != nil {
		args = append(args, *filter.Priority)
		conditions = append(conditions, fmt.Sprintf("priority = $%d", len(args)))
	}

	args = append(args, filter.PageSize)
	limitPlaceholder := len(args)
	args = append(args, filter.Offset())
	offsetPlaceholder := len(args)

	query := fmt.Sprintf(`
		SELECT %s, COUNT(*) OVER() AS total_count
		FROM tasks
		WHERE %s
		ORDER BY priority DESC, created_at DESC
		LIMIT $%d OFFSET $%d`,
		taskColumns, strings.Join(conditions, " AND "), limitPlaceholder, offsetPlaceholder)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query tasks: %w", err)
	}
	defer rows.Close()

	items := make([]Task, 0, filter.PageSize)
	total := 0
	for rows.Next() {
		var t Task
		var rowTotal int
		err := rows.Scan(&t.ID, &t.Title, &t.Description, &t.Status,
			&t.Priority, &t.CreatedAt, &t.UpdatedAt, &t.DeletedAt, &rowTotal)
		if err != nil {
			return nil, 0, fmt.Errorf("scan task row: %w", err)
		}
		total = rowTotal
		items = append(items, t)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate task rows: %w", err)
	}

	// An empty page past the end still needs the real total so the client can
	// tell "no results" from "page out of range".
	if len(items) == 0 {
		total, err = r.count(ctx, conditions, args[:len(args)-2])
		if err != nil {
			return nil, 0, err
		}
	}

	return items, total, nil
}

func (r *PostgresRepository) count(ctx context.Context, conditions []string, args []any) (int, error) {
	query := "SELECT COUNT(*) FROM tasks WHERE " + strings.Join(conditions, " AND ")
	var total int
	if err := r.pool.QueryRow(ctx, query, args...).Scan(&total); err != nil {
		return 0, fmt.Errorf("count tasks: %w", err)
	}
	return total, nil
}

// GetByID returns a single live task.
func (r *PostgresRepository) GetByID(ctx context.Context, id string) (Task, error) {
	query := "SELECT " + taskColumns + " FROM tasks WHERE id = $1 AND deleted_at IS NULL"

	var t Task
	err := r.pool.QueryRow(ctx, query, id).Scan(&t.ID, &t.Title, &t.Description,
		&t.Status, &t.Priority, &t.CreatedAt, &t.UpdatedAt, &t.DeletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Task{}, ErrNotFound
	}
	if err != nil {
		return Task{}, fmt.Errorf("get task %s: %w", id, err)
	}
	return t, nil
}

// Create inserts a task and returns the stored row.
func (r *PostgresRepository) Create(ctx context.Context, task Task) (Task, error) {
	query := `
		INSERT INTO tasks (id, title, description, status, priority)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING ` + taskColumns

	var t Task
	err := r.pool.QueryRow(ctx, query,
		task.ID, task.Title, task.Description, string(task.Status), task.Priority,
	).Scan(&t.ID, &t.Title, &t.Description, &t.Status,
		&t.Priority, &t.CreatedAt, &t.UpdatedAt, &t.DeletedAt)
	if err != nil {
		return Task{}, fmt.Errorf("insert task: %w", err)
	}
	return t, nil
}

// Update applies a partial update and returns the stored row.
//
// Only the fields present in the input appear in the SET clause, so a partial
// update can never blank a column the caller did not mention.
func (r *PostgresRepository) Update(ctx context.Context, id string, input UpdateInput) (Task, error) {
	assignments := []string{}
	args := []any{}

	if input.Title != nil {
		args = append(args, *input.Title)
		assignments = append(assignments, fmt.Sprintf("title = $%d", len(args)))
	}
	if input.Description != nil {
		args = append(args, *input.Description)
		assignments = append(assignments, fmt.Sprintf("description = $%d", len(args)))
	}
	if input.Status != nil {
		args = append(args, string(*input.Status))
		assignments = append(assignments, fmt.Sprintf("status = $%d", len(args)))
	}
	if input.Priority != nil {
		args = append(args, *input.Priority)
		assignments = append(assignments, fmt.Sprintf("priority = $%d", len(args)))
	}
	if len(assignments) == 0 {
		return r.GetByID(ctx, id)
	}

	args = append(args, id)
	query := fmt.Sprintf(
		"UPDATE tasks SET %s WHERE id = $%d AND deleted_at IS NULL RETURNING %s",
		strings.Join(assignments, ", "), len(args), taskColumns)

	var t Task
	err := r.pool.QueryRow(ctx, query, args...).Scan(&t.ID, &t.Title, &t.Description,
		&t.Status, &t.Priority, &t.CreatedAt, &t.UpdatedAt, &t.DeletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Task{}, ErrNotFound
	}
	if err != nil {
		return Task{}, fmt.Errorf("update task %s: %w", id, err)
	}
	return t, nil
}

// SoftDelete marks a task deleted without removing the row.
//
// Tasks carry an audit trail, so deletion is a state change. Hard deletes are
// reserved for data that must legally disappear.
func (r *PostgresRepository) SoftDelete(ctx context.Context, id string) error {
	query := "UPDATE tasks SET deleted_at = NOW() WHERE id = $1 AND deleted_at IS NULL"

	tag, err := r.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("delete task %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
