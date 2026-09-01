package tasks

import (
	"errors"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"

	"github.com/teo-garcia/gin-template-monolith/internal/shared/httpx"
)

// Handler is the HTTP boundary for tasks.
//
// Handlers only translate: parse input, call the service, respond. Business
// rules live in the service, SQL lives in the repository, and error rendering
// lives in the error middleware.
type Handler struct {
	service *Service
}

// NewHandler builds a task handler.
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// Register mounts the task routes under the API prefix group.
func (h *Handler) Register(group *gin.RouterGroup) {
	tasks := group.Group("/tasks")
	tasks.GET("", h.List)
	tasks.POST("", h.Create)
	tasks.GET("/:id", h.Get)
	tasks.PATCH("/:id", h.Update)
	tasks.DELETE("/:id", h.Delete)
}

// List handles GET /api/v1/tasks.
func (h *Handler) List(c *gin.Context) {
	filter := ListFilter{
		Page:     queryInt(c, "page", DefaultPage),
		PageSize: queryInt(c, "pageSize", DefaultPageSize),
	}
	if raw := c.Query("status"); raw != "" {
		status := Status(raw)
		filter.Status = &status
	}
	if raw := c.Query("priority"); raw != "" {
		priority, err := strconv.Atoi(raw)
		if err != nil {
			_ = c.Error(httpx.NewValidationError("Invalid query parameters", map[string]string{
				"priority": "must be an integer",
			}))
			return
		}
		filter.Priority = &priority
	}

	items, total, err := h.service.List(c.Request.Context(), filter)
	if err != nil {
		_ = c.Error(err)
		return
	}

	filter.Normalize()
	httpx.OK(c, httpx.PaginatedResponse[Task]{
		Data: items,
		Meta: httpx.PaginationMeta{
			Total:    total,
			Page:     filter.Page,
			PageSize: filter.PageSize,
		},
	})
}

// Get handles GET /api/v1/tasks/:id.
func (h *Handler) Get(c *gin.Context) {
	task, err := h.service.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		_ = c.Error(err)
		return
	}
	httpx.OK(c, task)
}

// Create handles POST /api/v1/tasks.
func (h *Handler) Create(c *gin.Context) {
	var input CreateInput
	if err := c.ShouldBindJSON(&input); err != nil {
		_ = c.Error(bindingError(err))
		return
	}

	task, err := h.service.Create(c.Request.Context(), input)
	if err != nil {
		_ = c.Error(err)
		return
	}
	httpx.Created(c, task)
}

// Update handles PATCH /api/v1/tasks/:id.
func (h *Handler) Update(c *gin.Context) {
	var input UpdateInput
	if err := c.ShouldBindJSON(&input); err != nil {
		_ = c.Error(bindingError(err))
		return
	}

	task, err := h.service.Update(c.Request.Context(), c.Param("id"), input)
	if err != nil {
		_ = c.Error(err)
		return
	}
	httpx.OK(c, task)
}

// Delete handles DELETE /api/v1/tasks/:id.
func (h *Handler) Delete(c *gin.Context) {
	if err := h.service.Delete(c.Request.Context(), c.Param("id")); err != nil {
		_ = c.Error(err)
		return
	}
	httpx.NoContent(c)
}

// queryInt reads an integer query parameter, falling back to a default.
//
// Unparseable pagination is treated as absent rather than as an error: it is a
// hint about which slice of results to return, not domain input.
func queryInt(c *gin.Context, name string, fallback int) int {
	raw := c.Query(name)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

// bindingError turns a Gin binding failure into a field-level validation error.
//
// Malformed JSON is a 400 (the request itself is broken); a well-formed body
// that violates a rule is a 422 with per-field detail.
func bindingError(err error) error {
	var validationErrs validator.ValidationErrors
	if errors.As(err, &validationErrs) {
		fields := make(map[string]string, len(validationErrs))
		for _, fieldErr := range validationErrs {
			fields[jsonFieldName(fieldErr)] = describeRule(fieldErr)
		}
		return httpx.NewValidationError("Validation failed", fields)
	}
	return httpx.NewBadRequestError("Malformed request body")
}

// jsonFieldName prefers the struct's JSON name so the error keys match the
// field names the client actually sent.
func jsonFieldName(fieldErr validator.FieldError) string {
	if name := fieldErr.Field(); name != "" {
		return name
	}
	return fieldErr.StructField()
}

func describeRule(fieldErr validator.FieldError) string {
	switch fieldErr.Tag() {
	case "required":
		return "is required"
	case "min":
		return "must be at least " + fieldErr.Param()
	case "max":
		return "must be at most " + fieldErr.Param()
	case "oneof":
		return "must be one of " + fieldErr.Param()
	default:
		return "failed the " + fieldErr.Tag() + " rule"
	}
}
