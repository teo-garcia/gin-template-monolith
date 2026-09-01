package middleware

import (
	"log/slog"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// quietPaths are scraped or polled constantly; logging them would bury real
// traffic under health-check noise.
var quietPaths = []string{"/health", "/metrics"}

// Logger emits one structured line per request.
//
// The level tracks the outcome: 5xx is an error, 4xx is a warning, everything
// else is info. That means an alert on `level=error` is meaningful without any
// further filtering.
func Logger(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		c.Next()

		if isQuiet(path) {
			return
		}

		status := c.Writer.Status()
		attrs := []any{
			slog.String("requestId", GetRequestID(c)),
			slog.String("method", c.Request.Method),
			slog.String("path", path),
			slog.String("route", c.FullPath()),
			slog.Int("status", status),
			slog.Duration("duration", time.Since(start)),
			slog.Int("bytes", c.Writer.Size()),
			slog.String("ip", c.ClientIP()),
		}
		if query != "" {
			attrs = append(attrs, slog.String("query", query))
		}

		ctx := c.Request.Context()
		switch {
		case status >= 500:
			logger.ErrorContext(ctx, "request completed", attrs...)
		case status >= 400:
			logger.WarnContext(ctx, "request completed", attrs...)
		default:
			logger.InfoContext(ctx, "request completed", attrs...)
		}
	}
}

func isQuiet(path string) bool {
	for _, prefix := range quietPaths {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}
