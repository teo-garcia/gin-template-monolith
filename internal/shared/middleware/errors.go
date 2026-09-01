package middleware

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/teo-garcia/gin-template-monolith/internal/shared/httpx"
)

// Context keys shared with the httpx responders.
const (
	apiVersionKey = "apiVersion"
)

// Context seeds the per-request values the responders and loggers need.
func Context(apiVersion string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(startTimeKey, time.Now())
		c.Set(apiVersionKey, apiVersion)
		c.Next()
	}
}

// ErrorHandler turns anything a handler reported via c.Error into the
// portfolio error envelope.
//
// This is the single place a failure becomes a response body. Handlers never
// serialize errors themselves, so the wire contract cannot drift per route.
func ErrorHandler(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		if len(c.Errors) == 0 {
			return
		}
		// A handler that already wrote a body wins; double-writing would corrupt it.
		if c.Writer.Written() {
			return
		}

		err := c.Errors.Last().Err
		apiErr := translate(err)

		attrs := []any{
			slog.String("requestId", GetRequestID(c)),
			slog.String("method", c.Request.Method),
			slog.String("path", c.Request.URL.Path),
			slog.Int("status", apiErr.StatusCode),
			slog.String("error", apiErr.Name),
		}
		if apiErr.StatusCode >= http.StatusInternalServerError {
			// 5xx keeps the full chain server-side; the client sees only the
			// client-safe message.
			logger.ErrorContext(c.Request.Context(), "request failed",
				append(attrs, slog.String("cause", err.Error()))...)
		} else {
			logger.WarnContext(c.Request.Context(), "request rejected",
				append(attrs, slog.String("detail", apiErr.Message))...)
		}

		httpx.RespondError(c, apiErr)
	}
}

// translate maps an arbitrary error onto a client-safe APIError.
//
// Anything unrecognized becomes a bare 500: never leak driver text, SQL, or
// stack detail to a client.
func translate(err error) *httpx.APIError {
	if apiErr, ok := httpx.AsAPIError(err); ok {
		return apiErr
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return httpx.NewTimeoutError("Request timed out")
	case errors.Is(err, context.Canceled):
		return httpx.NewBadRequestError("Request canceled by the client")
	default:
		return httpx.NewInternalError("Internal server error")
	}
}

// Recovery converts a panic into a 500 in the standard error envelope.
//
// Without this a panic would kill the connection with no body and no
// correlation ID, which is the least debuggable possible failure.
func Recovery(logger *slog.Logger) gin.HandlerFunc {
	return gin.CustomRecoveryWithWriter(nil, func(c *gin.Context, recovered any) {
		logger.ErrorContext(c.Request.Context(), "panic recovered",
			slog.String("requestId", GetRequestID(c)),
			slog.String("method", c.Request.Method),
			slog.String("path", c.Request.URL.Path),
			slog.Any("panic", recovered),
		)
		httpx.RespondError(c, httpx.NewInternalError("Internal server error"))
	})
}

// NotFound renders unmatched routes in the same envelope as everything else.
func NotFound() gin.HandlerFunc {
	return func(c *gin.Context) {
		httpx.RespondError(c, httpx.NewNotFoundError("Route not found"))
	}
}

// MethodNotAllowed renders a wrong-verb request in the standard envelope.
func MethodNotAllowed() gin.HandlerFunc {
	return func(c *gin.Context) {
		httpx.RespondError(c, &httpx.APIError{
			StatusCode: http.StatusMethodNotAllowed,
			Name:       "MethodNotAllowedError",
			Message:    "Method not allowed for this route",
		})
	}
}
