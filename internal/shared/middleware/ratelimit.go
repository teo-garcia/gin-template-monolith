package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"github.com/teo-garcia/gin-template-monolith/internal/config"
	"github.com/teo-garcia/gin-template-monolith/internal/shared/httpx"
)

// throttleSkip lists the operational routes that must answer even while a
// client is being throttled. Health and metrics are scraped continuously, and
// throttling them would take the service out of a load balancer under load.
var throttleSkip = []string{"/health", "/metrics", "/docs", "/openapi.json"}

// Limiter counts requests per client within a fixed window.
type Limiter interface {
	// Allow records a hit and reports the resulting count and window reset.
	Allow(ctx context.Context, key string, limit int, window time.Duration) (count int, reset time.Time, err error)
}

// RedisLimiter is the shared, multi-replica limiter.
//
// It uses a fixed window: INCR the key, and set the TTL on the first hit. That
// is the same algorithm the Nest and Adonis templates use, so the limit means
// the same thing across the portfolio.
type RedisLimiter struct {
	client *redis.Client
}

// NewRedisLimiter builds a limiter backed by Redis.
func NewRedisLimiter(client *redis.Client) *RedisLimiter {
	return &RedisLimiter{client: client}
}

// Allow implements Limiter.
func (l *RedisLimiter) Allow(
	ctx context.Context, key string, _ int, window time.Duration,
) (int, time.Time, error) {
	pipe := l.client.TxPipeline()
	incr := pipe.Incr(ctx, key)
	// NX so an in-flight window is never extended by a later request, which
	// would let a steady stream of traffic hold the window open forever.
	pipe.ExpireNX(ctx, key, window)
	ttl := pipe.TTL(ctx, key)

	if _, err := pipe.Exec(ctx); err != nil {
		return 0, time.Time{}, err
	}

	remaining := ttl.Val()
	if remaining <= 0 {
		remaining = window
	}
	return int(incr.Val()), time.Now().Add(remaining), nil
}

// MemoryLimiter is the fallback used when Redis is not configured, and in
// tests. It is per-process, so it does not enforce a global limit across
// replicas — that is why Redis is the default in every deployed environment.
type MemoryLimiter struct {
	mu      sync.Mutex
	windows map[string]*memWindow
}

type memWindow struct {
	count   int
	expires time.Time
}

// NewMemoryLimiter builds an in-process limiter.
func NewMemoryLimiter() *MemoryLimiter {
	return &MemoryLimiter{windows: make(map[string]*memWindow)}
}

// Allow implements Limiter.
func (l *MemoryLimiter) Allow(
	_ context.Context, key string, _ int, window time.Duration,
) (int, time.Time, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	w, ok := l.windows[key]
	if !ok || now.After(w.expires) {
		w = &memWindow{expires: now.Add(window)}
		l.windows[key] = w
	}
	w.count++

	// Opportunistically drop expired windows so a long-lived process does not
	// accumulate one map entry per client IP forever.
	if len(l.windows) > 1024 {
		for k, v := range l.windows {
			if now.After(v.expires) {
				delete(l.windows, k)
			}
		}
	}

	return w.count, w.expires, nil
}

// Throttle limits requests per client IP and emits the X-RateLimit-* headers.
//
// If the limiter backend itself fails the request is allowed through: a Redis
// outage must not take the whole API down, and the failure is logged so the
// degradation is visible.
func Throttle(limiter Limiter, cfg config.Config, logger *slog.Logger) gin.HandlerFunc {
	limit := cfg.Throttle.Limit
	window := cfg.Throttle.Window()

	return func(c *gin.Context) {
		if isThrottleExempt(c.Request.URL.Path) {
			c.Next()
			return
		}

		key := "throttle:" + c.ClientIP()
		count, reset, err := limiter.Allow(c.Request.Context(), key, limit, window)
		if err != nil {
			logger.WarnContext(c.Request.Context(), "rate limiter unavailable, allowing request",
				slog.String("requestId", GetRequestID(c)),
				slog.String("error", err.Error()))
			c.Next()
			return
		}

		remaining := max(limit-count, 0)
		h := c.Writer.Header()
		h.Set("X-RateLimit-Limit", strconv.Itoa(limit))
		h.Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
		h.Set("X-RateLimit-Reset", strconv.FormatInt(reset.Unix(), 10))

		if count > limit {
			retryAfter := max(int(time.Until(reset).Seconds()), 1)
			h.Set("Retry-After", strconv.Itoa(retryAfter))
			httpx.RespondError(c, httpx.NewRateLimitError(
				"Too many requests, please try again later"))
			return
		}

		c.Next()
	}
}

func isThrottleExempt(path string) bool {
	if path == "/" {
		return true
	}
	for _, prefix := range throttleSkip {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// Timeout aborts a request that outlives the configured deadline.
//
// The deadline is attached to the request context so database queries and
// outbound calls unwind with it instead of running on after the client is gone.
func Timeout(d time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		if isThrottleExempt(c.Request.URL.Path) {
			c.Next()
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), d)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)

		c.Next()

		if ctx.Err() != nil && !c.Writer.Written() {
			httpx.RespondError(c, &httpx.APIError{
				StatusCode: http.StatusGatewayTimeout,
				Name:       httpx.ErrTimeout,
				Message:    "Request timed out",
			})
		}
	}
}
