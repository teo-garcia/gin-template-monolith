package tasks

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
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

// taskRecord is the GORM persistence model. Keeping database annotations out of
// Task leaves the domain and HTTP representation independent of the ORM.
type taskRecord struct {
	ID          string         `gorm:"column:id;primaryKey"`
	Title       string         `gorm:"column:title"`
	Description *string        `gorm:"column:description"`
	Status      Status         `gorm:"column:status"`
	Priority    int            `gorm:"column:priority"`
	CreatedAt   time.Time      `gorm:"column:created_at;->"`
	UpdatedAt   time.Time      `gorm:"column:updated_at;->"`
	DeletedAt   gorm.DeletedAt `gorm:"column:deleted_at;index"`
}

func (taskRecord) TableName() string { return "tasks" }

func recordFromTask(task Task) taskRecord {
	record := taskRecord{
		ID:          task.ID,
		Title:       task.Title,
		Description: task.Description,
		Status:      task.Status,
		Priority:    task.Priority,
	}
	if task.DeletedAt != nil {
		record.DeletedAt = gorm.DeletedAt{Time: *task.DeletedAt, Valid: true}
	}
	return record
}

func (r taskRecord) task() Task {
	var deletedAt *time.Time
	if r.DeletedAt.Valid {
		deletedAt = &r.DeletedAt.Time
	}
	return Task{
		ID:          r.ID,
		Title:       r.Title,
		Description: r.Description,
		Status:      r.Status,
		Priority:    r.Priority,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
		DeletedAt:   deletedAt,
	}
}

// GORMRepository persists tasks through GORM's PostgreSQL dialect.
type GORMRepository struct {
	db *gorm.DB
}

// NewGORMRepository builds a task repository over the shared ORM connection.
func NewGORMRepository(db *gorm.DB) *GORMRepository {
	return &GORMRepository{db: db}
}

// List returns one page of live tasks plus the total matching count.
func (r *GORMRepository) List(ctx context.Context, filter ListFilter) ([]Task, int, error) {
	filter.Normalize()

	query := r.db.WithContext(ctx).Model(&taskRecord{})
	if filter.Status != nil {
		query = query.Where("status = ?", *filter.Status)
	}
	if filter.Priority != nil {
		query = query.Where("priority >= ?", *filter.Priority)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count tasks: %w", err)
	}

	rows := make([]taskRecord, 0, filter.PageSize)
	if err := query.
		Order("priority DESC").
		Order("created_at DESC").
		Limit(filter.PageSize).
		Offset(filter.Offset()).
		Find(&rows).Error; err != nil {
		return nil, 0, fmt.Errorf("query tasks: %w", err)
	}

	items := make([]Task, len(rows))
	for i := range rows {
		items[i] = rows[i].task()
	}
	return items, int(total), nil
}

// GetByID returns a single live task.
func (r *GORMRepository) GetByID(ctx context.Context, id string) (Task, error) {
	var record taskRecord
	result := r.db.WithContext(ctx).Where("id = ?", id).First(&record)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return Task{}, ErrNotFound
	}
	if result.Error != nil {
		return Task{}, fmt.Errorf("get task %s: %w", id, result.Error)
	}
	return record.task(), nil
}

// Create inserts a task and returns the stored row, including database-managed
// timestamps.
func (r *GORMRepository) Create(ctx context.Context, task Task) (Task, error) {
	record := recordFromTask(task)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&record).Error; err != nil {
			return err
		}
		// created_at and updated_at belong to the database migration, not the
		// ORM. Read the stored row before commit so the response contains the
		// authoritative values and a failed read cannot leave a partial create.
		return tx.Where("id = ?", record.ID).First(&record).Error
	})
	if err != nil {
		return Task{}, fmt.Errorf("insert task: %w", err)
	}
	return record.task(), nil
}

// Update applies only fields present in the input and returns the stored row.
func (r *GORMRepository) Update(ctx context.Context, id string, input UpdateInput) (Task, error) {
	updates := make(map[string]any, 4)
	if input.Title != nil {
		updates["title"] = *input.Title
	}
	if input.Description != nil {
		updates["description"] = *input.Description
	}
	if input.Status != nil {
		updates["status"] = *input.Status
	}
	if input.Priority != nil {
		updates["priority"] = *input.Priority
	}
	if len(updates) == 0 {
		return r.GetByID(ctx, id)
	}

	var updated Task
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&taskRecord{}).Where("id = ?", id).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrNotFound
		}

		var record taskRecord
		if err := tx.Where("id = ?", id).First(&record).Error; err != nil {
			return err
		}
		updated = record.task()
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, ErrNotFound) {
		return Task{}, ErrNotFound
	}
	if err != nil {
		return Task{}, fmt.Errorf("update task %s: %w", id, err)
	}
	return updated, nil
}

// SoftDelete marks a task deleted without removing the row.
//
// GORM's soft-delete scope automatically excludes deleted rows from every
// repository query. Hard deletes remain an explicit, exceptional operation.
func (r *GORMRepository) SoftDelete(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).Where("id = ?", id).Delete(&taskRecord{})
	if result.Error != nil {
		return fmt.Errorf("delete task %s: %w", id, result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
