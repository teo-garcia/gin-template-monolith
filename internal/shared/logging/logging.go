// Package logging builds the service logger.
//
// Structured JSON is the default so log shippers (Loki via Alloy in the local
// observability stack) get parseable input. Development can opt into a plain
// text handler, which is the only place human-readable output is worth the
// loss of structure.
package logging

import (
	"log/slog"
	"os"
	"strings"

	"github.com/teo-garcia/gin-template-monolith/internal/config"
)

// New builds the application logger from config.
func New(cfg config.Config) *slog.Logger {
	opts := &slog.HandlerOptions{Level: ParseLevel(cfg.Log.Level)}

	var handler slog.Handler
	if cfg.Log.JSON {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	return slog.New(handler).With(
		slog.String("service", cfg.OTel.ServiceName),
		slog.String("env", cfg.App.Env),
	)
}

// ParseLevel maps a LOG_LEVEL value onto a slog level.
//
// An unrecognized value falls back to info rather than failing: config
// validation has already rejected bad values, and a logger that refuses to
// build would take down the process for a cosmetic setting.
func ParseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
