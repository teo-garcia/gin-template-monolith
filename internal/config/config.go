// Package config loads and validates every environment variable the service
// needs, once, at startup.
//
// Nothing else in the service reads os.Getenv: a missing or malformed variable
// must fail the process before it starts serving, not on the first request that
// happens to touch it.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
)

// Config is the fully validated runtime configuration.
type Config struct {
	App      App
	Database Database
	Redis    Redis
	CORS     CORS
	Throttle Throttle
	Metrics  Metrics
	OTel     OTel
	Log      Log
}

// App holds process-level settings.
type App struct {
	Env             string        `env:"APP_ENV"          envDefault:"development"`
	Name            string        `env:"APP_NAME"         envDefault:"Gin Monolith Template"`
	Port            int           `env:"PORT"             envDefault:"3000"`
	APIPrefix       string        `env:"API_PREFIX"       envDefault:"/api/v1"`
	Version         string        `env:"API_VERSION"      envDefault:"1"`
	ShutdownTimeout time.Duration `env:"SHUTDOWN_TIMEOUT" envDefault:"10s"`
	RequestTimeout  time.Duration `env:"REQUEST_TIMEOUT"  envDefault:"30s"`
	DocsEnabled     bool          `env:"DOCS_ENABLED"     envDefault:"true"`
	OpenAPIServer   string        `env:"OPENAPI_SERVER_URL" envDefault:"http://localhost:3000"`
}

// Database holds Postgres connection and pool settings.
type Database struct {
	URL         string        `env:"DATABASE_URL,required,notEmpty"`
	PoolMax     int32         `env:"DATABASE_POOL_MAX"      envDefault:"10"`
	PoolMin     int32         `env:"DATABASE_POOL_MIN"      envDefault:"0"`
	MaxLifetime time.Duration `env:"DATABASE_MAX_LIFETIME"  envDefault:"1h"`
	ConnTimeout time.Duration `env:"DATABASE_CONN_TIMEOUT"  envDefault:"5s"`
}

// Redis holds cache and rate-limit backend settings.
type Redis struct {
	Host     string        `env:"REDIS_HOST"     envDefault:"localhost"`
	Port     int           `env:"REDIS_PORT"     envDefault:"6379"`
	Password string        `env:"REDIS_PASSWORD" envDefault:""`
	DB       int           `env:"REDIS_DB"       envDefault:"0"`
	TTL      time.Duration `env:"REDIS_TTL"      envDefault:"3600s"`
}

// Addr renders the host:port the Redis client dials.
func (r Redis) Addr() string {
	return fmt.Sprintf("%s:%d", r.Host, r.Port)
}

// CORS holds cross-origin settings. Defaults are restrictive on purpose.
type CORS struct {
	Enabled bool     `env:"CORS_ENABLED" envDefault:"true"`
	Origins []string `env:"CORS_ORIGIN"  envDefault:"http://localhost:3000" envSeparator:","`
}

// Throttle holds rate-limit settings.
type Throttle struct {
	// TTL is the window length in seconds; Limit is requests per window per IP.
	TTL   int `env:"THROTTLE_TTL"   envDefault:"60"`
	Limit int `env:"THROTTLE_LIMIT" envDefault:"100"`
}

// Window renders the throttle TTL as a duration.
func (t Throttle) Window() time.Duration {
	return time.Duration(t.TTL) * time.Second
}

// Metrics holds Prometheus settings.
type Metrics struct {
	Enabled bool `env:"METRICS_ENABLED" envDefault:"true"`
}

// OTel holds tracing settings.
type OTel struct {
	Enabled     bool   `env:"OTEL_ENABLED"      envDefault:"false"`
	ServiceName string `env:"OTEL_SERVICE_NAME" envDefault:"gin-template-monolith"`
	Endpoint    string `env:"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT" envDefault:"http://localhost:4318/v1/traces"`
}

// Log holds logging settings.
type Log struct {
	Level string `env:"LOG_LEVEL" envDefault:"info"`
	// JSON is forced on outside development so log shippers get structured input.
	JSON bool `env:"LOG_JSON" envDefault:"true"`
}

// IsProduction reports whether the service is running in production mode.
func (c Config) IsProduction() bool { return c.App.Env == "production" }

// IsTest reports whether the service is running under the test profile.
func (c Config) IsTest() bool { return c.App.Env == "test" }

// Load reads the environment into a Config and validates it.
//
// It returns every validation failure at once rather than the first, so a
// misconfigured deployment surfaces all its problems in a single log line.
func Load() (Config, error) {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return Config{}, fmt.Errorf("read environment: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate reports every configuration value that would break the service.
func (c Config) Validate() error {
	var problems []error

	switch c.App.Env {
	case "development", "test", "staging", "production":
	default:
		problems = append(problems, fmt.Errorf(
			"APP_ENV %q must be development, test, staging, or production", c.App.Env))
	}

	if c.App.Port < 1 || c.App.Port > 65535 {
		problems = append(problems, fmt.Errorf("PORT %d is outside 1-65535", c.App.Port))
	}
	if !strings.HasPrefix(c.App.APIPrefix, "/") {
		problems = append(problems, fmt.Errorf("API_PREFIX %q must start with /", c.App.APIPrefix))
	}
	if c.App.ShutdownTimeout <= 0 {
		problems = append(problems, errors.New("SHUTDOWN_TIMEOUT must be positive"))
	}
	if c.App.RequestTimeout <= 0 {
		problems = append(problems, errors.New("REQUEST_TIMEOUT must be positive"))
	}

	// url.Parse accepts the empty string, so emptiness is checked explicitly:
	// a blank DATABASE_URL must stop the process, not surface as a connect
	// failure on the first request.
	if strings.TrimSpace(c.Database.URL) == "" {
		problems = append(problems, errors.New("DATABASE_URL must not be empty"))
	} else if _, err := url.Parse(c.Database.URL); err != nil {
		problems = append(problems, fmt.Errorf("DATABASE_URL is not a valid URL: %w", err))
	}
	if c.Database.PoolMax < 1 {
		problems = append(problems, fmt.Errorf("DATABASE_POOL_MAX %d must be at least 1", c.Database.PoolMax))
	}
	if c.Database.PoolMin < 0 || c.Database.PoolMin > c.Database.PoolMax {
		problems = append(problems, fmt.Errorf(
			"DATABASE_POOL_MIN %d must be between 0 and DATABASE_POOL_MAX (%d)",
			c.Database.PoolMin, c.Database.PoolMax))
	}

	if c.Throttle.TTL < 1 {
		problems = append(problems, fmt.Errorf("THROTTLE_TTL %d must be at least 1", c.Throttle.TTL))
	}
	if c.Throttle.Limit < 1 {
		problems = append(problems, fmt.Errorf("THROTTLE_LIMIT %d must be at least 1", c.Throttle.Limit))
	}

	// A wildcard origin plus credentials is the classic CORS foot-gun, and in
	// production it is never what anyone actually wants.
	for _, origin := range c.CORS.Origins {
		if strings.TrimSpace(origin) == "*" && c.IsProduction() {
			problems = append(problems, errors.New("CORS_ORIGIN must not be * in production"))
		}
	}

	switch strings.ToLower(c.Log.Level) {
	case "debug", "info", "warn", "error":
	default:
		problems = append(problems, fmt.Errorf(
			"LOG_LEVEL %q must be debug, info, warn, or error", c.Log.Level))
	}

	if c.OTel.Enabled {
		if _, err := url.Parse(c.OTel.Endpoint); err != nil {
			problems = append(problems, fmt.Errorf(
				"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT is not a valid URL: %w", err))
		}
	}

	return errors.Join(problems...)
}
