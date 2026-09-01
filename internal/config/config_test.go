package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/teo-garcia/gin-template-monolith/internal/config"
)

// valid returns a config that passes validation, so each test can break exactly
// one thing and assert that the break is what fails.
func valid() config.Config {
	cfg := config.Config{}
	cfg.App = config.App{
		Env: "development", Name: "Gin Monolith Template", Port: 3000,
		APIPrefix: "/api/v1", Version: "1",
		ShutdownTimeout: 10 * time.Second, RequestTimeout: 30 * time.Second,
		DocsEnabled: true, OpenAPIServer: "http://localhost:3000",
	}
	cfg.Database = config.Database{
		URL: "postgres://user:pass@localhost:5432/db", PoolMax: 10, PoolMin: 0,
	}
	cfg.Redis = config.Redis{Host: "localhost", Port: 6379}
	cfg.CORS = config.CORS{Enabled: true, Origins: []string{"http://localhost:3000"}}
	cfg.Throttle = config.Throttle{TTL: 60, Limit: 100}
	cfg.Log = config.Log{Level: "info", JSON: true}
	return cfg
}

func TestValidBaselinePasses(t *testing.T) {
	t.Parallel()

	if err := valid().Validate(); err != nil {
		t.Fatalf("the baseline config should validate, got: %v", err)
	}
}

func TestValidateRejectsBadValues(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		mutate func(*config.Config)
		want   string
	}{
		"unknown env":         {func(c *config.Config) { c.App.Env = "staging-2" }, "APP_ENV"},
		"port out of range":   {func(c *config.Config) { c.App.Port = 70000 }, "PORT"},
		"port zero":           {func(c *config.Config) { c.App.Port = 0 }, "PORT"},
		"prefix without /":    {func(c *config.Config) { c.App.APIPrefix = "api/v1" }, "API_PREFIX"},
		"zero shutdown":       {func(c *config.Config) { c.App.ShutdownTimeout = 0 }, "SHUTDOWN_TIMEOUT"},
		"zero request":        {func(c *config.Config) { c.App.RequestTimeout = 0 }, "REQUEST_TIMEOUT"},
		"pool max below one":  {func(c *config.Config) { c.Database.PoolMax = 0 }, "DATABASE_POOL_MAX"},
		"pool min above max":  {func(c *config.Config) { c.Database.PoolMin = 50 }, "DATABASE_POOL_MIN"},
		"throttle ttl zero":   {func(c *config.Config) { c.Throttle.TTL = 0 }, "THROTTLE_TTL"},
		"throttle limit zero": {func(c *config.Config) { c.Throttle.Limit = 0 }, "THROTTLE_LIMIT"},
		"unknown log level":   {func(c *config.Config) { c.Log.Level = "trace" }, "LOG_LEVEL"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cfg := valid()
			tc.mutate(&cfg)

			err := cfg.Validate()
			if err == nil {
				t.Fatalf("expected %s to fail validation", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %s", err, tc.want)
			}
		})
	}
}

// A wildcard origin in production is the classic CORS foot-gun.
func TestValidateRejectsWildcardCORSInProductionOnly(t *testing.T) {
	t.Parallel()

	t.Run("rejected in production", func(t *testing.T) {
		t.Parallel()
		cfg := valid()
		cfg.App.Env = "production"
		cfg.CORS.Origins = []string{"*"}

		if err := cfg.Validate(); err == nil {
			t.Fatal("CORS_ORIGIN=* must not validate in production")
		}
	})

	t.Run("allowed in development", func(t *testing.T) {
		t.Parallel()
		cfg := valid()
		cfg.CORS.Origins = []string{"*"}

		if err := cfg.Validate(); err != nil {
			t.Errorf("CORS_ORIGIN=* should be allowed locally, got: %v", err)
		}
	})
}

// Validate must report every problem at once so a misconfigured deployment
// surfaces all of them in one log line rather than one per restart.
func TestValidateReportsEveryProblemAtOnce(t *testing.T) {
	t.Parallel()

	cfg := valid()
	cfg.App.Port = 0
	cfg.Log.Level = "trace"
	cfg.Throttle.Limit = 0

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation to fail")
	}
	for _, want := range []string{"PORT", "LOG_LEVEL", "THROTTLE_LIMIT"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("aggregated error is missing %s:\n%v", want, err)
		}
	}
}

func TestLoadReadsTheEnvironment(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db")
	t.Setenv("APP_ENV", "test")
	t.Setenv("PORT", "4100")
	t.Setenv("CORS_ORIGIN", "http://a.example,http://b.example")
	t.Setenv("THROTTLE_LIMIT", "250")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.App.Port != 4100 {
		t.Errorf("PORT = %d, want 4100", cfg.App.Port)
	}
	if !cfg.IsTest() {
		t.Error("IsTest() should be true for APP_ENV=test")
	}
	// Comma-separated origins must split into a list.
	if len(cfg.CORS.Origins) != 2 {
		t.Errorf("CORS origins = %v, want two entries", cfg.CORS.Origins)
	}
	if cfg.Throttle.Limit != 250 {
		t.Errorf("THROTTLE_LIMIT = %d, want 250", cfg.Throttle.Limit)
	}
	// Defaults fill in for anything unset.
	if cfg.App.APIPrefix != "/api/v1" {
		t.Errorf("API_PREFIX = %q, want the default /api/v1", cfg.App.APIPrefix)
	}
}

// DATABASE_URL has no safe default, so its absence must stop the process.
func TestLoadFailsWithoutDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")

	if _, err := config.Load(); err == nil {
		t.Fatal("Load succeeded without DATABASE_URL")
	}
}

func TestLoadRejectsInvalidEnvironment(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db")
	t.Setenv("LOG_LEVEL", "verbose")

	if _, err := config.Load(); err == nil {
		t.Fatal("Load succeeded with an invalid LOG_LEVEL")
	}
}

func TestRedisAddr(t *testing.T) {
	t.Parallel()

	cfg := valid()
	cfg.Redis = config.Redis{Host: "redis", Port: 6380}

	if got := cfg.Redis.Addr(); got != "redis:6380" {
		t.Errorf("Addr() = %q, want redis:6380", got)
	}
}

func TestThrottleWindow(t *testing.T) {
	t.Parallel()

	cfg := valid()
	cfg.Throttle.TTL = 90

	if got := cfg.Throttle.Window(); got != 90*time.Second {
		t.Errorf("Window() = %v, want 90s", got)
	}
}
