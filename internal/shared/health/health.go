// Package health implements the portfolio health contract:
// GET /health, GET /health/live, GET /health/ready.
//
// The payload is the same contract the frontend templates parse in
// `lib/health.ts` and the Nest and Adonis backends emit:
//
//	{status, timestamp, version, checks{name: "up"|"down"}}
//
// It deliberately does not follow any one framework's health output (Terminus,
// Actuator): those are library artifacts, not a contract anyone chose. Health
// responses are also NOT wrapped in the success envelope — orchestrators and
// load balancers parse them, and should not have to unwrap an
// application-level envelope to find `status`.
package health

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/teo-garcia/gin-template-monolith/internal/shared/database"
)

// Overall status values.
const (
	StatusOK       = "ok"
	StatusDegraded = "degraded"
	StatusDown     = "down"
)

// Per-dependency check states.
const (
	CheckUp   = "up"
	CheckDown = "down"
)

// checkTimeout bounds every dependency probe. A readiness check that hangs is
// worse than one that fails: the orchestrator learns nothing and waits.
const checkTimeout = 2 * time.Second

// Response is the health payload.
type Response struct {
	Status    string            `json:"status"`
	Timestamp string            `json:"timestamp"`
	Version   string            `json:"version"`
	Checks    map[string]string `json:"checks,omitempty"`
}

// Checker probes the service's dependencies.
type Checker struct {
	pool    *pgxpool.Pool
	redis   *redis.Client
	version string
}

// NewChecker builds a checker. Either dependency may be nil, in which case it
// is simply not reported.
func NewChecker(pool *pgxpool.Pool, rdb *redis.Client, version string) *Checker {
	return &Checker{pool: pool, redis: rdb, version: version}
}

// resolveStatus aggregates dependency checks into the overall status:
// every check up -> ok, some up -> degraded, none up -> down.
func resolveStatus(checks map[string]string) string {
	if len(checks) == 0 {
		return StatusOK
	}

	up, down := 0, 0
	for _, state := range checks {
		if state == CheckUp {
			up++
		} else {
			down++
		}
	}

	switch {
	case down == 0:
		return StatusOK
	case up == 0:
		return StatusDown
	default:
		return StatusDegraded
	}
}

// Check probes every configured dependency and aggregates the result.
func (c *Checker) Check(ctx context.Context) Response {
	checks := map[string]string{}

	if c.pool != nil {
		checks["database"] = c.checkDatabase(ctx)
	}
	if c.redis != nil {
		checks["redis"] = c.checkRedis(ctx)
	}

	return Response{
		Status:    resolveStatus(checks),
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Version:   c.version,
		Checks:    checks,
	}
}

func (c *Checker) checkDatabase(ctx context.Context) string {
	if err := database.Ping(ctx, c.pool, checkTimeout); err != nil {
		return CheckDown
	}
	return CheckUp
}

func (c *Checker) checkRedis(ctx context.Context) string {
	pingCtx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	if err := c.redis.Ping(pingCtx).Err(); err != nil {
		return CheckDown
	}
	return CheckUp
}

// Handler serves GET /health.
func (c *Checker) Handler() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		writeHealth(ctx, c.Check(ctx.Request.Context()))
	}
}

// Ready serves GET /health/ready.
//
// It reports the same status and the same HTTP code as /health. Returning
// `degraded` on one and `error` on the other for the same failure was drift,
// not a feature.
func (c *Checker) Ready() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		writeHealth(ctx, c.Check(ctx.Request.Context()))
	}
}

// Live serves GET /health/live.
//
// Liveness answers "is this process still running", so it deliberately does not
// touch the database or Redis: a dependency outage must not cause the
// orchestrator to restart an otherwise healthy process. It reports no `checks`
// for the same reason.
func (c *Checker) Live() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		ctx.JSON(http.StatusOK, Response{
			Status:    StatusOK,
			Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
			Version:   c.version,
		})
	}
}

// writeHealth answers 200 only when everything is up; degraded and down both
// answer 503 so orchestrators drain the instance.
func writeHealth(c *gin.Context, resp Response) {
	code := http.StatusOK
	if resp.Status != StatusOK {
		code = http.StatusServiceUnavailable
	}
	c.JSON(code, resp)
}
