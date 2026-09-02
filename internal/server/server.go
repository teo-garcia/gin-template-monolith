// Package server wires configuration, dependencies, and middleware into an
// http.Handler.
//
// Keeping assembly here rather than in main means the whole routing tree can be
// built in a test with fakes, which is what makes the e2e suite possible
// without a live process.
package server

import (
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"

	"github.com/teo-garcia/gin-template-monolith/internal/config"
	"github.com/teo-garcia/gin-template-monolith/internal/modules/tasks"
	"github.com/teo-garcia/gin-template-monolith/internal/shared/health"
	"github.com/teo-garcia/gin-template-monolith/internal/shared/httpx"
	"github.com/teo-garcia/gin-template-monolith/internal/shared/metrics"
	"github.com/teo-garcia/gin-template-monolith/internal/shared/middleware"
	"github.com/teo-garcia/gin-template-monolith/internal/shared/openapi"
)

// Dependencies are the external resources the router needs.
//
// Every field is optional so tests can build a router with only what they
// exercise; the corresponding routes degrade rather than panic.
type Dependencies struct {
	Pool    *pgxpool.Pool
	Redis   *redis.Client
	Logger  *slog.Logger
	Metrics *metrics.Registry
	// Limiter overrides the rate-limit backend. Defaults to Redis when a client
	// is present, and to an in-process limiter otherwise.
	Limiter middleware.Limiter
	// TaskRepository overrides persistence, so handler tests need no database.
	TaskRepository tasks.Repository
}

// New builds the fully wired HTTP handler.
func New(cfg config.Config, deps Dependencies) *gin.Engine {
	configureGinGlobals(cfg)

	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}

	engine := gin.New()
	// Unmatched routes and wrong verbs must return the standard envelope, not
	// Gin's plain-text default.
	engine.HandleMethodNotAllowed = true
	engine.NoRoute(middleware.NotFound())
	engine.NoMethod(middleware.MethodNotAllowed())
	// Only trust forwarding headers from the reverse proxy in front of us.
	_ = engine.SetTrustedProxies(nil)

	// Order matters: correlation first so everything downstream can log the
	// request id; recovery early so a panic in any later middleware is caught;
	// the error handler before the routes it renders errors for.
	engine.Use(middleware.RequestID())
	engine.Use(middleware.Context(cfg.App.Version))
	engine.Use(middleware.Recovery(logger))
	engine.Use(middleware.SecurityHeaders())
	engine.Use(middleware.CORS(cfg))
	engine.Use(middleware.Logger(logger))

	if cfg.OTel.Enabled {
		engine.Use(otelgin.Middleware(cfg.OTel.ServiceName))
	}
	if deps.Metrics != nil && cfg.Metrics.Enabled {
		engine.Use(deps.Metrics.Middleware())
	}

	engine.Use(middleware.Throttle(resolveLimiter(deps), cfg, logger))
	engine.Use(middleware.Timeout(cfg.App.RequestTimeout))
	engine.Use(middleware.ErrorHandler(logger))

	registerSystemRoutes(engine, cfg, deps)
	registerAPIRoutes(engine, cfg, deps)

	return engine
}

func resolveLimiter(deps Dependencies) middleware.Limiter {
	if deps.Limiter != nil {
		return deps.Limiter
	}
	if deps.Redis != nil {
		return middleware.NewRedisLimiter(deps.Redis)
	}
	return middleware.NewMemoryLimiter()
}

func registerSystemRoutes(engine *gin.Engine, cfg config.Config, deps Dependencies) {
	checker := health.NewChecker(deps.Pool, deps.Redis, cfg.App.Version)
	engine.GET("/health", checker.Handler())
	engine.GET("/health/live", checker.Live())
	engine.GET("/health/ready", checker.Ready())

	// {name, status, version} is the shared service-info shape every template
	// answers with; env and docs are Go-template extras.
	engine.GET("/", func(c *gin.Context) {
		httpx.OK(c, gin.H{
			"name":    cfg.App.Name,
			"status":  "ok",
			"version": cfg.App.Version,
			"env":     cfg.App.Env,
			"docs":    docsPath(cfg),
		})
	})

	if deps.Metrics != nil && cfg.Metrics.Enabled {
		engine.GET("/metrics", deps.Metrics.Handler())
	}

	if cfg.App.DocsEnabled {
		spec := openapi.Build(cfg)
		engine.GET("/openapi.json", func(c *gin.Context) {
			c.JSON(http.StatusOK, spec)
		})
		engine.GET("/docs", func(c *gin.Context) {
			c.Data(http.StatusOK, "text/html; charset=utf-8", docsHTML)
		})
	}
}

func registerAPIRoutes(engine *gin.Engine, cfg config.Config, deps Dependencies) {
	repo := deps.TaskRepository
	if repo == nil {
		if deps.Pool == nil {
			// Without persistence there is nothing to serve; the system routes
			// above still work, which is what health checks need.
			return
		}
		repo = tasks.NewPostgresRepository(deps.Pool)
	}

	api := engine.Group(cfg.App.APIPrefix)
	tasks.NewHandler(tasks.NewService(repo)).Register(api)
}

func docsPath(cfg config.Config) string {
	if !cfg.App.DocsEnabled {
		return ""
	}
	return "/docs"
}

// ginGlobals serializes the two pieces of process-wide state New touches.
//
// Gin keeps its mode and its validator in package-level variables. A server
// built concurrently with another — which is exactly what parallel tests do —
// would otherwise race on both. Production builds one server, so this costs
// nothing there and makes the test suite honest.
var ginGlobals struct {
	mu                sync.Mutex
	validatorPrepared bool
}

func configureGinGlobals(cfg config.Config) {
	ginGlobals.mu.Lock()
	defer ginGlobals.mu.Unlock()

	if cfg.IsProduction() || cfg.IsTest() {
		gin.SetMode(gin.ReleaseMode)
	}

	// Registering the tag-name function mutates the shared validator, so it
	// happens exactly once per process.
	if ginGlobals.validatorPrepared {
		return
	}
	v, ok := binding.Validator.Engine().(*validator.Validate)
	if !ok {
		return
	}
	// Report the JSON field name the client sent (`title`) rather than the Go
	// struct field (`Title`), so error keys match the request body.
	v.RegisterTagNameFunc(func(field reflect.StructField) string {
		name := strings.SplitN(field.Tag.Get("json"), ",", 2)[0]
		if name == "-" {
			return ""
		}
		if name == "" {
			return field.Name
		}
		return name
	})
	ginGlobals.validatorPrepared = true
}
