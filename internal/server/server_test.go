package server_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/teo-garcia/gin-template-monolith/internal/config"
	"github.com/teo-garcia/gin-template-monolith/internal/modules/tasks"
	"github.com/teo-garcia/gin-template-monolith/internal/server"
	"github.com/teo-garcia/gin-template-monolith/internal/shared/metrics"
	"github.com/teo-garcia/gin-template-monolith/internal/shared/middleware"
)

// These tests exercise the assembled router, so they assert the wire contract
// every other backend template in the portfolio also emits.

func testConfig() config.Config {
	cfg := config.Config{}
	cfg.App = config.App{
		Env: "test", Name: "Gin Monolith Template", Port: 3000,
		APIPrefix: "/api/v1", Version: "1",
		ShutdownTimeout: 10 * time.Second, RequestTimeout: 30 * time.Second,
		DocsEnabled: true, OpenAPIServer: "http://localhost:3000",
	}
	cfg.CORS = config.CORS{Enabled: true, Origins: []string{"http://localhost:3000"}}
	// The portfolio test default is a high limit so unrelated tests are never
	// throttled by accident.
	cfg.Throttle = config.Throttle{TTL: 60, Limit: 1000}
	cfg.Metrics = config.Metrics{Enabled: true}
	cfg.Log = config.Log{Level: "error", JSON: true}
	return cfg
}

func newTestServer(t *testing.T, seed ...tasks.Task) http.Handler {
	t.Helper()
	return server.New(testConfig(), server.Dependencies{
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		Metrics:        metrics.New(),
		TaskRepository: newMemoryRepository(seed...),
	})
}

func do(t *testing.T, handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("response is not JSON: %v\nbody: %s", err, rec.Body.String())
	}
	return out
}

// --- Success envelope ---------------------------------------------------

func TestSuccessEnvelopeShape(t *testing.T) {
	t.Parallel()

	rec := do(t, newTestServer(t), http.MethodGet, "/api/v1/tasks?page=1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200\nbody: %s", rec.Code, rec.Body.String())
	}

	body := decode(t, rec)
	if body["success"] != true {
		t.Errorf("success = %v, want true", body["success"])
	}
	if body["statusCode"] != float64(200) {
		t.Errorf("statusCode = %v, want 200", body["statusCode"])
	}
	if body["method"] != http.MethodGet {
		t.Errorf("method = %v, want GET", body["method"])
	}
	// `path` must include the query string, matching the other templates.
	if body["path"] != "/api/v1/tasks?page=1" {
		t.Errorf("path = %v, want the path including the query string", body["path"])
	}
	if _, ok := body["timestamp"].(string); !ok {
		t.Error("timestamp is missing or not a string")
	}
	if _, ok := body["data"]; !ok {
		t.Error("data key is missing")
	}

	meta, ok := body["meta"].(map[string]any)
	if !ok {
		t.Fatal("meta is missing")
	}
	if meta["version"] != "1" {
		t.Errorf("meta.version = %v, want 1", meta["version"])
	}
	if id, _ := meta["requestId"].(string); id == "" {
		t.Error("meta.requestId is empty; correlation would be impossible")
	}
}

func TestPaginationContract(t *testing.T) {
	t.Parallel()

	handler := newTestServer(t,
		tasks.Task{ID: "a", Title: "A", Status: tasks.StatusPending},
		tasks.Task{ID: "b", Title: "B", Status: tasks.StatusCompleted},
	)

	rec := do(t, handler, http.MethodGet, "/api/v1/tasks?page=2&pageSize=250", "")

	data, ok := decode(t, rec)["data"].(map[string]any)
	if !ok {
		t.Fatal("data is not an object; list endpoints return {data, meta}")
	}
	meta, ok := data["meta"].(map[string]any)
	if !ok {
		t.Fatal("data.meta is missing")
	}

	if meta["page"] != float64(2) {
		t.Errorf("meta.page = %v, want 2", meta["page"])
	}
	// pageSize must be capped at the maximum, not echoed back.
	if meta["pageSize"] != float64(tasks.MaxPageSize) {
		t.Errorf("meta.pageSize = %v, want it capped at %d", meta["pageSize"], tasks.MaxPageSize)
	}
	if _, ok := meta["total"]; !ok {
		t.Error("meta.total is missing")
	}
}

// --- Error envelope -----------------------------------------------------

func TestErrorEnvelopeShape(t *testing.T) {
	t.Parallel()

	rec := do(t, newTestServer(t), http.MethodGet, "/api/v1/tasks/does-not-exist", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404\nbody: %s", rec.Code, rec.Body.String())
	}

	body := decode(t, rec)
	if body["success"] != false {
		t.Errorf("success = %v, want false", body["success"])
	}
	if body["statusCode"] != float64(404) {
		t.Errorf("statusCode = %v, want 404", body["statusCode"])
	}
	if body["error"] != "NotFoundError" {
		t.Errorf("error = %v, want NotFoundError", body["error"])
	}
	if _, ok := body["message"].(string); !ok {
		t.Error("message is missing")
	}
	meta, ok := body["meta"].(map[string]any)
	if !ok {
		t.Fatal("meta is missing on the error envelope")
	}
	if id, _ := meta["requestId"].(string); id == "" {
		t.Error("meta.requestId is empty on the error path")
	}
}

func TestValidationErrorCarriesFieldDetail(t *testing.T) {
	t.Parallel()

	rec := do(t, newTestServer(t), http.MethodPost, "/api/v1/tasks", `{"description":"no title"}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422\nbody: %s", rec.Code, rec.Body.String())
	}

	body := decode(t, rec)
	if body["error"] != "ValidationError" {
		t.Errorf("error = %v, want ValidationError", body["error"])
	}
	fields, ok := body["errors"].(map[string]any)
	if !ok {
		t.Fatal("errors is missing; validation failures must name the bad fields")
	}
	// The key must be the JSON name the client sent, not the Go struct field.
	if _, ok := fields["title"]; !ok {
		t.Errorf("errors = %v, want a lowercase `title` key", fields)
	}
}

func TestMalformedJSONIsBadRequestNotValidation(t *testing.T) {
	t.Parallel()

	rec := do(t, newTestServer(t), http.MethodPost, "/api/v1/tasks", `{"title":`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for unparseable JSON", rec.Code)
	}
	if got := decode(t, rec)["error"]; got != "BadRequestError" {
		t.Errorf("error = %v, want BadRequestError", got)
	}
}

func TestUnknownRouteUsesTheErrorEnvelope(t *testing.T) {
	t.Parallel()

	rec := do(t, newTestServer(t), http.MethodGet, "/nope", "")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	body := decode(t, rec)
	if body["success"] != false || body["error"] != "NotFoundError" {
		t.Errorf("unmatched routes must use the standard envelope, got %v", body)
	}
}

func TestWrongMethodUsesTheErrorEnvelope(t *testing.T) {
	t.Parallel()

	rec := do(t, newTestServer(t), http.MethodPut, "/api/v1/tasks", "")

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	if got := decode(t, rec)["error"]; got != "MethodNotAllowedError" {
		t.Errorf("error = %v, want MethodNotAllowedError", got)
	}
}

// --- CRUD round trip ----------------------------------------------------

func TestTaskLifecycle(t *testing.T) {
	t.Parallel()
	handler := newTestServer(t)

	created := do(t, handler, http.MethodPost, "/api/v1/tasks",
		`{"title":"Ship it","priority":7,"status":"IN_PROGRESS"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201\nbody: %s", created.Code, created.Body.String())
	}
	task, ok := decode(t, created)["data"].(map[string]any)
	if !ok {
		t.Fatal("create response has no data object")
	}
	id, _ := task["id"].(string)
	if id == "" {
		t.Fatal("created task has no id")
	}
	if task["priority"] != float64(7) || task["status"] != "IN_PROGRESS" {
		t.Errorf("created task did not keep its input: %v", task)
	}

	got := do(t, handler, http.MethodGet, "/api/v1/tasks/"+id, "")
	if got.Code != http.StatusOK {
		t.Fatalf("get status = %d, want 200", got.Code)
	}

	patched := do(t, handler, http.MethodPatch, "/api/v1/tasks/"+id, `{"status":"COMPLETED"}`)
	if patched.Code != http.StatusOK {
		t.Fatalf("patch status = %d, want 200\nbody: %s", patched.Code, patched.Body.String())
	}
	updated, _ := decode(t, patched)["data"].(map[string]any)
	if updated["status"] != "COMPLETED" {
		t.Errorf("status = %v, want COMPLETED", updated["status"])
	}
	if updated["title"] != "Ship it" {
		t.Errorf("title = %v, want it untouched by a partial update", updated["title"])
	}

	deleted := do(t, handler, http.MethodDelete, "/api/v1/tasks/"+id, "")
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204", deleted.Code)
	}
	// A 204 carries no body per the portfolio contract.
	if deleted.Body.Len() != 0 {
		t.Errorf("204 response has a body: %s", deleted.Body.String())
	}

	if after := do(t, handler, http.MethodGet, "/api/v1/tasks/"+id, ""); after.Code != http.StatusNotFound {
		t.Errorf("status after delete = %d, want 404", after.Code)
	}
}

// --- System routes ------------------------------------------------------

func TestHealthEndpointsAreNotEnveloped(t *testing.T) {
	t.Parallel()
	handler := newTestServer(t)

	for _, path := range []string{"/health", "/health/live", "/health/ready"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			rec := do(t, handler, http.MethodGet, path, "")

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 with no dependencies configured", rec.Code)
			}
			body := decode(t, rec)
			if body["status"] != "ok" {
				t.Errorf("status = %v, want ok", body["status"])
			}
			// Orchestrators parse these directly; wrapping would break them.
			if _, wrapped := body["success"]; wrapped {
				t.Error("health responses must not be wrapped in the success envelope")
			}
			// Shared portfolio contract: status + timestamp + version.
			if _, ok := body["timestamp"].(string); !ok {
				t.Errorf("timestamp = %v, want a string", body["timestamp"])
			}
			if _, ok := body["version"].(string); !ok {
				t.Errorf("version = %v, want a string", body["version"])
			}
		})
	}
}

// Liveness answers "is the process up". Probing dependencies here would let a
// database outage trigger container restarts, so it must report no checks.
func TestLivenessReportsNoDependencyChecks(t *testing.T) {
	t.Parallel()

	rec := do(t, newTestServer(t), http.MethodGet, "/health/live", "")
	body := decode(t, rec)

	if _, present := body["checks"]; present {
		t.Error("liveness must not run dependency checks")
	}
}

// /health and /health/ready must never disagree: reporting different statuses
// for the same failure was drift in an earlier template.
func TestHealthAndReadyAgree(t *testing.T) {
	t.Parallel()
	handler := newTestServer(t)

	overall := decode(t, do(t, handler, http.MethodGet, "/health", ""))
	ready := decode(t, do(t, handler, http.MethodGet, "/health/ready", ""))

	if overall["status"] != ready["status"] {
		t.Errorf("/health status = %v, /health/ready status = %v; must agree",
			overall["status"], ready["status"])
	}
}

func TestMetricsEndpointServesPrometheusText(t *testing.T) {
	t.Parallel()
	handler := newTestServer(t)

	// Generate one request so the counter has a sample.
	do(t, handler, http.MethodGet, "/api/v1/tasks", "")
	rec := do(t, handler, http.MethodGet, "/metrics", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"http_requests_total", "http_request_duration_seconds_bucket", "go_goroutines",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics output is missing %q", want)
		}
	}
	// The route label must be the pattern, never a concrete id.
	if !strings.Contains(body, `route="/api/v1/tasks"`) {
		t.Errorf("expected a templated route label in:\n%s", body)
	}
}

func TestMetricsUseTheRoutePatternNotTheRawPath(t *testing.T) {
	t.Parallel()
	handler := newTestServer(t, tasks.Task{ID: "abc-123", Title: "A"})

	do(t, handler, http.MethodGet, "/api/v1/tasks/abc-123", "")
	body := do(t, handler, http.MethodGet, "/metrics", "").Body.String()

	if strings.Contains(body, "abc-123") {
		t.Error("a task id leaked into a metric label; that is unbounded cardinality")
	}
	if !strings.Contains(body, `route="/api/v1/tasks/:id"`) {
		t.Errorf("expected the :id route pattern in:\n%s", body)
	}
}

func TestOpenAPIDocumentsTheEnvelopeNotTheBarePayload(t *testing.T) {
	t.Parallel()

	rec := do(t, newTestServer(t), http.MethodGet, "/openapi.json", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var doc struct {
		OpenAPI    string `json:"openapi"`
		Components struct {
			Schemas map[string]any `json:"schemas"`
		} `json:"components"`
		Paths map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("openapi.json is not valid JSON: %v", err)
	}

	if !strings.HasPrefix(doc.OpenAPI, "3.") {
		t.Errorf("openapi = %q, want a 3.x document", doc.OpenAPI)
	}
	for _, schema := range []string{"SuccessEnvelope", "ErrorEnvelope", "Task", "PaginatedTasks"} {
		if _, ok := doc.Components.Schemas[schema]; !ok {
			t.Errorf("components.schemas is missing %q", schema)
		}
	}

	// The 200 response must reference the envelope, not a bare Task.
	get, ok := doc.Paths["/api/v1/tasks/{id}"]["get"].(map[string]any)
	if !ok {
		t.Fatal("GET /api/v1/tasks/{id} is not documented")
	}
	rendered, err := json.Marshal(get)
	if err != nil {
		t.Fatalf("marshal operation: %v", err)
	}
	if !strings.Contains(string(rendered), "SuccessEnvelope") {
		t.Errorf("the 200 response does not reference SuccessEnvelope:\n%s", rendered)
	}
}

func TestDocsPageIsServed(t *testing.T) {
	t.Parallel()

	rec := do(t, newTestServer(t), http.MethodGet, "/docs", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	if !strings.Contains(rec.Body.String(), "/openapi.json") {
		t.Error("the docs page does not reference the spec it renders")
	}
}

func TestDocsAreDisabledByConfig(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	cfg.App.DocsEnabled = false
	handler := server.New(cfg, server.Dependencies{
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		TaskRepository: newMemoryRepository(),
	})

	for _, path := range []string{"/docs", "/openapi.json"} {
		if rec := do(t, handler, http.MethodGet, path, ""); rec.Code != http.StatusNotFound {
			t.Errorf("%s status = %d, want 404 when DOCS_ENABLED is false", path, rec.Code)
		}
	}
}

// --- Middleware ---------------------------------------------------------

func TestRequestIDIsGeneratedAndEchoed(t *testing.T) {
	t.Parallel()

	rec := do(t, newTestServer(t), http.MethodGet, "/health/live", "")

	if rec.Header().Get(middleware.RequestIDHeader) == "" {
		t.Error("no X-Request-ID on the response")
	}
}

func TestInboundRequestIDIsPreserved(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks", http.NoBody)
	req.Header.Set(middleware.RequestIDHeader, "trace-from-upstream")
	rec := httptest.NewRecorder()
	newTestServer(t).ServeHTTP(rec, req)

	if got := rec.Header().Get(middleware.RequestIDHeader); got != "trace-from-upstream" {
		t.Errorf("X-Request-ID = %q, want the inbound value echoed back", got)
	}
	meta, _ := decode(t, rec)["meta"].(map[string]any)
	if meta["requestId"] != "trace-from-upstream" {
		t.Errorf("meta.requestId = %v, want the inbound value", meta["requestId"])
	}
}

func TestSecurityHeaders(t *testing.T) {
	t.Parallel()

	rec := do(t, newTestServer(t), http.MethodGet, "/api/v1/tasks", "")

	want := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "no-referrer",
	}
	for header, value := range want {
		if got := rec.Header().Get(header); got != value {
			t.Errorf("%s = %q, want %q", header, got, value)
		}
	}
	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Error("Content-Security-Policy is missing")
	}
	// HSTS over plaintext would pin browsers to a scheme the dev server does
	// not serve.
	if got := rec.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("HSTS = %q, want it absent on a plaintext request", got)
	}
}

func TestCORSReflectsOnlyAllowedOrigins(t *testing.T) {
	t.Parallel()
	handler := newTestServer(t)

	t.Run("allowed origin is echoed", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks", http.NoBody)
		req.Header.Set("Origin", "http://localhost:3000")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
			t.Errorf("Allow-Origin = %q, want the allowed origin", got)
		}
		if !strings.Contains(rec.Header().Get("Vary"), "Origin") {
			t.Error("Vary: Origin is missing; caches could cross origins")
		}
	})

	t.Run("unlisted origin gets no CORS headers", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks", http.NoBody)
		req.Header.Set("Origin", "https://evil.example")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("Allow-Origin = %q, want empty for an unlisted origin", got)
		}
	})
}

func TestThrottleReturnsTheStandardEnvelopeAndHeaders(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	cfg.Throttle = config.Throttle{TTL: 60, Limit: 2}
	handler := server.New(cfg, server.Dependencies{
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		Limiter:        middleware.NewMemoryLimiter(),
		TaskRepository: newMemoryRepository(),
	})

	var last *httptest.ResponseRecorder
	for range 3 {
		last = do(t, handler, http.MethodGet, "/api/v1/tasks", "")
	}

	if last.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 after exceeding the limit", last.Code)
	}
	if got := decode(t, last)["error"]; got != "RateLimitError" {
		t.Errorf("error = %v, want RateLimitError", got)
	}
	for _, header := range []string{"X-RateLimit-Limit", "X-RateLimit-Remaining", "X-RateLimit-Reset", "Retry-After"} {
		if last.Header().Get(header) == "" {
			t.Errorf("%s is missing on the 429", header)
		}
	}
}

// Health and metrics are scraped continuously; throttling them would take the
// service out of a load balancer under load.
func TestThrottleExemptsOperationalRoutes(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	cfg.Throttle = config.Throttle{TTL: 60, Limit: 1}
	handler := server.New(cfg, server.Dependencies{
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		Limiter:        middleware.NewMemoryLimiter(),
		Metrics:        metrics.New(),
		TaskRepository: newMemoryRepository(),
	})

	for _, path := range []string{"/health", "/health/live", "/metrics"} {
		for range 5 {
			if rec := do(t, handler, http.MethodGet, path, ""); rec.Code == http.StatusTooManyRequests {
				t.Fatalf("%s was throttled; operational routes must stay reachable", path)
			}
		}
	}
}

// A limiter backend outage must degrade, not take the API down with it.
func TestThrottleFailsOpenWhenTheBackendErrors(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	handler := server.New(cfg, server.Dependencies{
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		Limiter:        brokenLimiter{},
		TaskRepository: newMemoryRepository(),
	})

	if rec := do(t, handler, http.MethodGet, "/api/v1/tasks", ""); rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; a limiter outage must fail open", rec.Code)
	}
}

func TestRootReportsServiceInfo(t *testing.T) {
	t.Parallel()

	rec := do(t, newTestServer(t), http.MethodGet, "/", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	data, ok := decode(t, rec)["data"].(map[string]any)
	if !ok {
		t.Fatal("root response has no data object")
	}
	if data["env"] != "test" || data["version"] != "1" {
		t.Errorf("service info = %v, want the configured env and version", data)
	}
}

type brokenLimiter struct{}

func (brokenLimiter) Allow(context.Context, string, int, time.Duration) (int, time.Time, error) {
	return 0, time.Time{}, context.DeadlineExceeded
}
