package httpx

import (
	"errors"
	"fmt"
	"net/http"
)

// Error names are the stable, machine-readable `error` field values. Clients
// branch on these, so they must not change when an internal type is renamed.
const (
	ErrValidation     = "ValidationError"
	ErrBadRequest     = "BadRequestError"
	ErrUnauthorized   = "UnauthorizedError"
	ErrForbidden      = "ForbiddenError"
	ErrNotFound       = "NotFoundError"
	ErrConflict       = "ConflictError"
	ErrRateLimit      = "RateLimitError"
	ErrTimeout        = "TimeoutError"
	ErrDatabase       = "DatabaseError"
	ErrInternalServer = "InternalServerError"
)

// APIError is a domain or application failure that maps cleanly onto a status
// code and a client-safe message.
//
// Handlers return these; the error middleware turns them into the wire
// envelope. Anything that is not an APIError is treated as an unknown failure
// and reported as a 500 with no internal detail leaked.
type APIError struct {
	StatusCode int
	Name       string
	Message    string
	// Fields carries per-field validation detail. It must stay client-safe.
	Fields map[string]string
	// cause is logged server-side and never serialized.
	cause error
}

func (e *APIError) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Name, e.Message, e.cause)
	}
	return fmt.Sprintf("%s: %s", e.Name, e.Message)
}

// Unwrap exposes the underlying cause to errors.Is and errors.As.
func (e *APIError) Unwrap() error { return e.cause }

// WithCause attaches an internal error for server-side logging.
//
// The cause is never serialized into a response.
func (e *APIError) WithCause(err error) *APIError {
	e.cause = err
	return e
}

// NewValidationError reports invalid input with per-field detail.
func NewValidationError(message string, fields map[string]string) *APIError {
	return &APIError{
		StatusCode: http.StatusUnprocessableEntity,
		Name:       ErrValidation,
		Message:    message,
		Fields:     fields,
	}
}

// NewBadRequestError reports a malformed request.
func NewBadRequestError(message string) *APIError {
	return &APIError{StatusCode: http.StatusBadRequest, Name: ErrBadRequest, Message: message}
}

// NewNotFoundError reports a missing resource.
func NewNotFoundError(message string) *APIError {
	return &APIError{StatusCode: http.StatusNotFound, Name: ErrNotFound, Message: message}
}

// NewConflictError reports a uniqueness or state conflict.
func NewConflictError(message string) *APIError {
	return &APIError{StatusCode: http.StatusConflict, Name: ErrConflict, Message: message}
}

// NewUnauthorizedError reports missing or invalid credentials.
func NewUnauthorizedError(message string) *APIError {
	return &APIError{StatusCode: http.StatusUnauthorized, Name: ErrUnauthorized, Message: message}
}

// NewForbiddenError reports an authenticated but unpermitted request.
func NewForbiddenError(message string) *APIError {
	return &APIError{StatusCode: http.StatusForbidden, Name: ErrForbidden, Message: message}
}

// NewRateLimitError reports a throttled request.
func NewRateLimitError(message string) *APIError {
	return &APIError{StatusCode: http.StatusTooManyRequests, Name: ErrRateLimit, Message: message}
}

// NewTimeoutError reports a request that exceeded its deadline.
func NewTimeoutError(message string) *APIError {
	return &APIError{StatusCode: http.StatusRequestTimeout, Name: ErrTimeout, Message: message}
}

// NewDatabaseError reports a persistence failure with a client-safe message.
func NewDatabaseError(message string) *APIError {
	return &APIError{StatusCode: http.StatusInternalServerError, Name: ErrDatabase, Message: message}
}

// NewInternalError reports an unexpected failure.
func NewInternalError(message string) *APIError {
	return &APIError{
		StatusCode: http.StatusInternalServerError,
		Name:       ErrInternalServer,
		Message:    message,
	}
}

// AsAPIError extracts an *APIError from an error chain.
func AsAPIError(err error) (*APIError, bool) {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr, true
	}
	return nil, false
}
