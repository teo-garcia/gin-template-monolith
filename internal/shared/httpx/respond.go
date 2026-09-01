package httpx

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// Context keys the responders read. They are set by the middleware package but
// declared here so httpx has no import cycle back into middleware.
const (
	requestIDKey    = "requestID"
	startTimeKey    = "requestStart"
	apiVersionKey   = "apiVersion"
	envelopeUsedKey = "envelopeUsed"
)

// Respond writes data wrapped in the portfolio success envelope.
//
// Unlike the Nest and Spring templates, which wrap responses in an interceptor
// with a path skip-list, this template wraps at the call site. The wire format
// is identical; the difference is that /health, /metrics, and /docs simply do
// not call this function, so there is no skip-list to keep in sync.
func Respond(c *gin.Context, statusCode int, data any) {
	c.Set(envelopeUsedKey, true)
	c.JSON(statusCode, NewSuccessEnvelope(
		statusCode,
		fullPath(c),
		c.Request.Method,
		data,
		requestID(c),
		apiVersion(c),
		elapsed(c),
	))
}

// OK writes a 200 with the success envelope.
func OK(c *gin.Context, data any) { Respond(c, http.StatusOK, data) }

// Created writes a 201 with the success envelope.
func Created(c *gin.Context, data any) { Respond(c, http.StatusCreated, data) }

// NoContent writes a 204. Per the portfolio contract a 204 carries no body, so
// it is deliberately not enveloped.
func NoContent(c *gin.Context) {
	c.Set(envelopeUsedKey, true)
	c.Status(http.StatusNoContent)
}

// RespondError writes an APIError as the portfolio error envelope.
//
// Handlers normally do not call this directly: they return the error via
// c.Error and let the ErrorHandler middleware render it.
func RespondError(c *gin.Context, apiErr *APIError) {
	c.Set(envelopeUsedKey, true)
	c.AbortWithStatusJSON(apiErr.StatusCode, NewErrorEnvelope(
		apiErr.StatusCode,
		fullPath(c),
		c.Request.Method,
		apiErr.Message,
		apiErr.Name,
		apiErr.Fields,
		requestID(c),
	))
}

// fullPath returns the request path including the query string, matching the
// `path` field the other backend templates emit.
func fullPath(c *gin.Context) string {
	if c.Request.URL.RawQuery != "" {
		return c.Request.URL.Path + "?" + c.Request.URL.RawQuery
	}
	return c.Request.URL.Path
}

func requestID(c *gin.Context) string {
	if v, ok := c.Get(requestIDKey); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func apiVersion(c *gin.Context) string {
	if v, ok := c.Get(apiVersionKey); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func elapsed(c *gin.Context) time.Duration {
	if v, ok := c.Get(startTimeKey); ok {
		if start, ok := v.(time.Time); ok {
			return time.Since(start)
		}
	}
	return 0
}
