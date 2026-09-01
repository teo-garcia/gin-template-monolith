// Package middleware holds the cross-cutting request pipeline: correlation,
// logging, the response envelope, error translation, CORS, security headers,
// rate limiting, and timeouts.
package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	// RequestIDHeader is the portfolio-wide correlation header.
	RequestIDHeader = "X-Request-ID"
	// RequestIDKey is the Gin context key the rest of the service reads.
	RequestIDKey = "requestID"
	// startTimeKey records when the request entered the pipeline.
	startTimeKey = "requestStart"
)

// RequestID adopts an inbound X-Request-ID or generates one, exposes it on the
// context for logs and envelopes, and echoes it back on the response.
//
// Accepting the client's value is what makes a trace stitch together across
// services; generating one guarantees every log line has something to join on.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(RequestIDHeader)
		if id == "" {
			id = uuid.NewString()
		}
		c.Set(RequestIDKey, id)
		c.Header(RequestIDHeader, id)
		c.Next()
	}
}

// GetRequestID returns the correlation ID for the current request.
func GetRequestID(c *gin.Context) string {
	if id, ok := c.Get(RequestIDKey); ok {
		if s, ok := id.(string); ok {
			return s
		}
	}
	return ""
}
