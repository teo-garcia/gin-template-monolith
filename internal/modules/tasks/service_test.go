package tasks_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/teo-garcia/gin-template-monolith/internal/modules/tasks"
	"github.com/teo-garcia/gin-template-monolith/internal/shared/httpx"
)

// fakeRepository is an in-memory Repository. The service layer holds the
// business rules, so it is tested without a database.
type fakeRepository struct {
	items map[string]tasks.Task
	// failWith, when set, is returned by every method. It stands in for an
	// arbitrary infrastructure failure.
	failWith error
	lastList tasks.ListFilter
}

func newFakeRepository(seed ...tasks.Task) *fakeRepository {
	repo := &fakeRepository{items: map[string]tasks.Task{}}
	for _, t := range seed {
		repo.items[t.ID] = t
	}
	return repo
}

func (r *fakeRepository) List(_ context.Context, filter tasks.ListFilter) ([]tasks.Task, int, error) {
	if r.failWith != nil {
		return nil, 0, r.failWith
	}
	r.lastList = filter
	out := make([]tasks.Task, 0, len(r.items))
	for _, t := range r.items {
		if filter.Status != nil && t.Status != *filter.Status {
			continue
		}
		if filter.Priority != nil && t.Priority != *filter.Priority {
			continue
		}
		out = append(out, t)
	}
	return out, len(out), nil
}

func (r *fakeRepository) GetByID(_ context.Context, id string) (tasks.Task, error) {
	if r.failWith != nil {
		return tasks.Task{}, r.failWith
	}
	t, ok := r.items[id]
	if !ok {
		return tasks.Task{}, tasks.ErrNotFound
	}
	return t, nil
}

func (r *fakeRepository) Create(_ context.Context, task tasks.Task) (tasks.Task, error) {
	if r.failWith != nil {
		return tasks.Task{}, r.failWith
	}
	r.items[task.ID] = task
	return task, nil
}

func (r *fakeRepository) Update(_ context.Context, id string, input tasks.UpdateInput) (tasks.Task, error) {
	if r.failWith != nil {
		return tasks.Task{}, r.failWith
	}
	t, ok := r.items[id]
	if !ok {
		return tasks.Task{}, tasks.ErrNotFound
	}
	if input.Title != nil {
		t.Title = *input.Title
	}
	if input.Description != nil {
		t.Description = input.Description
	}
	if input.Status != nil {
		t.Status = *input.Status
	}
	if input.Priority != nil {
		t.Priority = *input.Priority
	}
	r.items[id] = t
	return t, nil
}

func (r *fakeRepository) SoftDelete(_ context.Context, id string) error {
	if r.failWith != nil {
		return r.failWith
	}
	if _, ok := r.items[id]; !ok {
		return tasks.ErrNotFound
	}
	delete(r.items, id)
	return nil
}

func ptr[T any](v T) *T { return &v }

func TestCreateAppliesDefaults(t *testing.T) {
	t.Parallel()

	svc := tasks.NewService(newFakeRepository())

	created, err := svc.Create(t.Context(), tasks.CreateInput{Title: "Write docs"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	if created.Status != tasks.StatusPending {
		t.Errorf("status = %q, want PENDING by default", created.Status)
	}
	if created.Priority != 0 {
		t.Errorf("priority = %d, want 0 by default", created.Priority)
	}
	if created.ID == "" {
		t.Error("Create did not assign an id")
	}
}

func TestCreateTrimsTitleAndNormalizesBlankDescription(t *testing.T) {
	t.Parallel()

	svc := tasks.NewService(newFakeRepository())

	created, err := svc.Create(t.Context(), tasks.CreateInput{
		Title:       "  Trim me  ",
		Description: ptr("   "),
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	if created.Title != "Trim me" {
		t.Errorf("title = %q, want the trimmed value", created.Title)
	}
	// A whitespace-only description should be NULL, not an empty string.
	if created.Description != nil {
		t.Errorf("description = %q, want nil for a whitespace-only value", *created.Description)
	}
}

func TestCreateRejectsBlankTitle(t *testing.T) {
	t.Parallel()

	svc := tasks.NewService(newFakeRepository())

	_, err := svc.Create(t.Context(), tasks.CreateInput{Title: "   "})

	apiErr := requireAPIError(t, err)
	if apiErr.StatusCode != http.StatusUnprocessableEntity || apiErr.Name != httpx.ErrValidation {
		t.Errorf("got %d/%s, want 422/ValidationError", apiErr.StatusCode, apiErr.Name)
	}
	if _, ok := apiErr.Fields["title"]; !ok {
		t.Errorf("fields = %v, want a `title` entry", apiErr.Fields)
	}
}

func TestCreateRejectsUnknownStatus(t *testing.T) {
	t.Parallel()

	svc := tasks.NewService(newFakeRepository())

	_, err := svc.Create(t.Context(), tasks.CreateInput{
		Title:  "Valid",
		Status: ptr(tasks.Status("ARCHIVED")),
	})

	apiErr := requireAPIError(t, err)
	if apiErr.Name != httpx.ErrValidation {
		t.Errorf("error = %s, want ValidationError", apiErr.Name)
	}
}

func TestGetMapsMissingRowToNotFound(t *testing.T) {
	t.Parallel()

	svc := tasks.NewService(newFakeRepository())

	_, err := svc.Get(t.Context(), "missing")

	apiErr := requireAPIError(t, err)
	if apiErr.StatusCode != http.StatusNotFound || apiErr.Name != httpx.ErrNotFound {
		t.Errorf("got %d/%s, want 404/NotFoundError", apiErr.StatusCode, apiErr.Name)
	}
}

// A repository failure must never reach the client as driver text.
func TestGetHidesRepositoryFailureDetail(t *testing.T) {
	t.Parallel()

	repo := newFakeRepository()
	repo.failWith = errors.New("pq: password authentication failed for user \"admin\"")
	svc := tasks.NewService(repo)

	_, err := svc.Get(t.Context(), "any")

	apiErr := requireAPIError(t, err)
	if apiErr.StatusCode != http.StatusInternalServerError || apiErr.Name != httpx.ErrDatabase {
		t.Errorf("got %d/%s, want 500/DatabaseError", apiErr.StatusCode, apiErr.Name)
	}
	if apiErr.Message != "Could not load task" {
		t.Errorf("message = %q, want a generic client-safe message", apiErr.Message)
	}
	// The cause is still available for server-side logging.
	if !errors.Is(err, repo.failWith) {
		t.Error("the underlying cause was not preserved for logging")
	}
}

func TestUpdateRejectsAnEmptyPatch(t *testing.T) {
	t.Parallel()

	svc := tasks.NewService(newFakeRepository(tasks.Task{ID: "t1", Title: "Original"}))

	_, err := svc.Update(t.Context(), "t1", tasks.UpdateInput{})

	apiErr := requireAPIError(t, err)
	if apiErr.Name != httpx.ErrValidation {
		t.Errorf("error = %s, want ValidationError for an empty patch", apiErr.Name)
	}
}

func TestUpdateAppliesOnlyProvidedFields(t *testing.T) {
	t.Parallel()

	repo := newFakeRepository(tasks.Task{
		ID: "t1", Title: "Original", Status: tasks.StatusPending, Priority: 3,
	})
	svc := tasks.NewService(repo)

	updated, err := svc.Update(t.Context(), "t1", tasks.UpdateInput{
		Status: ptr(tasks.StatusCompleted),
	})
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}

	if updated.Status != tasks.StatusCompleted {
		t.Errorf("status = %q, want COMPLETED", updated.Status)
	}
	// Fields absent from the patch must survive untouched.
	if updated.Title != "Original" {
		t.Errorf("title = %q, want it left alone by a partial update", updated.Title)
	}
	if updated.Priority != 3 {
		t.Errorf("priority = %d, want it left alone by a partial update", updated.Priority)
	}
}

func TestUpdateRejectsBlankTitle(t *testing.T) {
	t.Parallel()

	svc := tasks.NewService(newFakeRepository(tasks.Task{ID: "t1", Title: "Original"}))

	_, err := svc.Update(t.Context(), "t1", tasks.UpdateInput{Title: ptr("   ")})

	if apiErr := requireAPIError(t, err); apiErr.Name != httpx.ErrValidation {
		t.Errorf("error = %s, want ValidationError", apiErr.Name)
	}
}

func TestDeleteMapsMissingRowToNotFound(t *testing.T) {
	t.Parallel()

	svc := tasks.NewService(newFakeRepository())

	err := svc.Delete(t.Context(), "missing")

	if apiErr := requireAPIError(t, err); apiErr.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", apiErr.StatusCode)
	}
}

func TestListRejectsUnknownStatusFilter(t *testing.T) {
	t.Parallel()

	svc := tasks.NewService(newFakeRepository())
	bad := tasks.Status("NOPE")

	_, _, err := svc.List(t.Context(), tasks.ListFilter{Status: &bad})

	if apiErr := requireAPIError(t, err); apiErr.Name != httpx.ErrValidation {
		t.Errorf("error = %s, want ValidationError", apiErr.Name)
	}
}

func TestListRejectsOutOfRangePriorityFilter(t *testing.T) {
	t.Parallel()

	svc := tasks.NewService(newFakeRepository())

	_, _, err := svc.List(t.Context(), tasks.ListFilter{Priority: ptr(99)})

	if apiErr := requireAPIError(t, err); apiErr.Name != httpx.ErrValidation {
		t.Errorf("error = %s, want ValidationError", apiErr.Name)
	}
}

// Pagination is clamped, not rejected: a client asking for page 0 or 5000 items
// wants results, not a 422.
func TestListClampsPagination(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name             string
		page, pageSize   int
		wantPage, wantSz int
	}{
		{"zero page falls back", 0, 10, tasks.DefaultPage, 10},
		{"negative page falls back", -5, 10, tasks.DefaultPage, 10},
		{"zero size falls back", 1, 0, 1, tasks.DefaultPageSize},
		{"oversized page is capped", 1, 5000, 1, tasks.MaxPageSize},
		{"in-range values pass through", 3, 50, 3, 50},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			repo := newFakeRepository()
			svc := tasks.NewService(repo)

			_, _, err := svc.List(t.Context(), tasks.ListFilter{
				Page: tc.page, PageSize: tc.pageSize,
			})
			if err != nil {
				t.Fatalf("List returned error: %v", err)
			}

			if repo.lastList.Page != tc.wantPage {
				t.Errorf("page = %d, want %d", repo.lastList.Page, tc.wantPage)
			}
			if repo.lastList.PageSize != tc.wantSz {
				t.Errorf("pageSize = %d, want %d", repo.lastList.PageSize, tc.wantSz)
			}
		})
	}
}

func TestListFilterOffset(t *testing.T) {
	t.Parallel()

	filter := tasks.ListFilter{Page: 3, PageSize: 20}

	if got := filter.Offset(); got != 40 {
		t.Errorf("Offset() = %d, want 40 for page 3 of 20", got)
	}
}

func TestStatusValid(t *testing.T) {
	t.Parallel()

	for _, s := range tasks.AllStatuses() {
		if !s.Valid() {
			t.Errorf("%q should be valid", s)
		}
	}
	if tasks.Status("MADE_UP").Valid() {
		t.Error("an unknown status must not validate")
	}
}

func requireAPIError(t *testing.T, err error) *httpx.APIError {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	apiErr, ok := httpx.AsAPIError(err)
	if !ok {
		t.Fatalf("error %v is not an *httpx.APIError; it would surface as a bare 500", err)
	}
	return apiErr
}
