// Command api is the service entrypoint.
//
// It loads config, opens dependencies, starts the HTTP server, and drains
// cleanly on SIGTERM.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/teo-garcia/gin-template-monolith/internal/config"
	"github.com/teo-garcia/gin-template-monolith/internal/server"
	"github.com/teo-garcia/gin-template-monolith/internal/shared/database"
	"github.com/teo-garcia/gin-template-monolith/internal/shared/logging"
	"github.com/teo-garcia/gin-template-monolith/internal/shared/metrics"
	"github.com/teo-garcia/gin-template-monolith/internal/shared/tracing"
)

func main() {
	if err := run(); err != nil {
		// The logger may not exist yet if config failed, so this one message
		// goes to stderr directly.
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}

	logger := logging.New(cfg)
	slog.SetDefault(logger)

	// Signals cancel this context, which unwinds startup and serving alike.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	shutdownTracing, err := tracing.Setup(ctx, cfg)
	if err != nil {
		return fmt.Errorf("configure tracing: %w", err)
	}

	pool, err := database.Connect(ctx, cfg)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()

	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr(),
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	defer func() {
		if err := rdb.Close(); err != nil {
			logger.Warn("closing redis client failed", slog.String("error", err.Error()))
		}
	}()

	// Redis backs rate limiting and caching, not correctness. A cold start with
	// Redis down should degrade, not refuse to boot.
	pingCtx, cancelPing := context.WithTimeout(ctx, 3*time.Second)
	if err := rdb.Ping(pingCtx).Err(); err != nil {
		logger.Warn("redis unreachable at startup; rate limiting will fail open",
			slog.String("addr", cfg.Redis.Addr()),
			slog.String("error", err.Error()))
	}
	cancelPing()

	handler := server.New(cfg, server.Dependencies{
		Pool:    pool,
		Redis:   rdb,
		Logger:  logger,
		Metrics: metrics.New(),
	})

	httpServer := &http.Server{
		// 0.0.0.0 so the container is reachable from outside its namespace.
		Addr:    net.JoinHostPort("0.0.0.0", fmt.Sprint(cfg.App.Port)),
		Handler: handler,
		// Bound every phase of a connection so a slow client cannot pin a
		// goroutine and a file descriptor indefinitely.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       cfg.App.RequestTimeout + 5*time.Second,
		WriteTimeout:      cfg.App.RequestTimeout + 10*time.Second,
		IdleTimeout:       120 * time.Second,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("server listening",
			slog.String("addr", httpServer.Addr),
			slog.String("apiPrefix", cfg.App.APIPrefix),
			slog.Bool("docs", cfg.App.DocsEnabled))

		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		return fmt.Errorf("http server failed: %w", err)
	case <-ctx.Done():
		logger.Info("shutdown signal received, draining connections",
			slog.Duration("timeout", cfg.App.ShutdownTimeout))
	}

	// A fresh context: the signal context is already canceled, and draining
	// needs its own budget.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.App.ShutdownTimeout)
	defer cancel()

	var shutdownErr error
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		shutdownErr = fmt.Errorf("drain http server: %w", err)
		logger.Error("graceful shutdown failed, closing forcibly",
			slog.String("error", err.Error()))
		_ = httpServer.Close()
	}
	if err := shutdownTracing(shutdownCtx); err != nil {
		logger.Warn("flushing traces failed", slog.String("error", err.Error()))
	}

	logger.Info("shutdown complete")
	return shutdownErr
}
