// Package httpx owns the wire contract every response in this service obeys.
//
// The shapes here are portfolio-wide, not Gin-specific: the Nest, Adonis,
// FastAPI, Django, and Spring templates all emit the same two envelopes, and a
// client should not be able to tell which one it is talking to.
package httpx

import "time"

// SuccessEnvelope wraps every 2xx response body.
//
//	{success, statusCode, timestamp, path, method, data, meta{requestId, version, duration}}
type SuccessEnvelope struct {
	Success    bool        `json:"success"`
	StatusCode int         `json:"statusCode"`
	Timestamp  string      `json:"timestamp"`
	Path       string      `json:"path"`
	Method     string      `json:"method"`
	Data       any         `json:"data"`
	Meta       SuccessMeta `json:"meta"`
}

// SuccessMeta carries correlation and timing data.
type SuccessMeta struct {
	RequestID string `json:"requestId,omitempty"`
	Version   string `json:"version"`
	// Milliseconds. A number, not a formatted string: every other template in
	// the portfolio emits meta.duration as a number and clients compare them.
	Duration int64 `json:"duration"`
}

// ErrorEnvelope wraps every 4xx and 5xx response body.
//
//	{success:false, statusCode, timestamp, path, method, message, error, errors?, meta{requestId}}
type ErrorEnvelope struct {
	Success    bool              `json:"success"`
	StatusCode int               `json:"statusCode"`
	Timestamp  string            `json:"timestamp"`
	Path       string            `json:"path"`
	Method     string            `json:"method"`
	Message    string            `json:"message"`
	Error      string            `json:"error"`
	Errors     map[string]string `json:"errors,omitempty"`
	Meta       ErrorMeta         `json:"meta"`
}

// ErrorMeta carries correlation data on the failure path.
type ErrorMeta struct {
	RequestID string `json:"requestId,omitempty"`
}

// PaginatedResponse is the portfolio list contract: `{data, meta{total, page, pageSize}}`.
//
// It is the `data` of a SuccessEnvelope, not a replacement for it.
type PaginatedResponse[T any] struct {
	Data []T            `json:"data"`
	Meta PaginationMeta `json:"meta"`
}

// PaginationMeta describes the page that was returned.
type PaginationMeta struct {
	Total    int `json:"total"`
	Page     int `json:"page"`
	PageSize int `json:"pageSize"`
}

// NewSuccessEnvelope builds a success envelope for a request.
func NewSuccessEnvelope(
	statusCode int, path, method string, data any, requestID, version string, duration time.Duration,
) SuccessEnvelope {
	return SuccessEnvelope{
		Success:    true,
		StatusCode: statusCode,
		Timestamp:  time.Now().UTC().Format(time.RFC3339Nano),
		Path:       path,
		Method:     method,
		Data:       data,
		Meta: SuccessMeta{
			RequestID: requestID,
			Version:   version,
			Duration:  duration.Milliseconds(),
		},
	}
}

// NewErrorEnvelope builds an error envelope for a request.
func NewErrorEnvelope(
	statusCode int, path, method, message, errorName string,
	fieldErrors map[string]string, requestID string,
) ErrorEnvelope {
	return ErrorEnvelope{
		Success:    false,
		StatusCode: statusCode,
		Timestamp:  time.Now().UTC().Format(time.RFC3339Nano),
		Path:       path,
		Method:     method,
		Message:    message,
		Error:      errorName,
		Errors:     fieldErrors,
		Meta:       ErrorMeta{RequestID: requestID},
	}
}
