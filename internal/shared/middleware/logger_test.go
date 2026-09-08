package middleware

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/trace"
)

func TestLoggerCorrelatesTraceWithoutSensitiveRequestData(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	router := gin.New()
	router.Use(Logger(logger))
	router.GET("/tasks/:id", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	traceID := trace.TraceID{1}
	spanID := trace.SpanID{2}
	spanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID,
		SpanID:  spanID,
	})
	request := httptest.NewRequest(http.MethodGet, "/tasks/123?token=secret", http.NoBody)
	request.RemoteAddr = "203.0.113.10:1234"
	request = request.WithContext(trace.ContextWithSpanContext(request.Context(), spanContext))
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	var event map[string]any
	if err := json.Unmarshal(output.Bytes(), &event); err != nil {
		t.Fatalf("decode log event: %v", err)
	}
	if event["trace_id"] != traceID.String() {
		t.Fatalf("trace_id = %v, want %s", event["trace_id"], traceID.String())
	}
	if event["span_id"] != spanID.String() {
		t.Fatalf("span_id = %v, want %s", event["span_id"], spanID.String())
	}
	if event["route"] != "/tasks/:id" {
		t.Fatalf("route = %v, want /tasks/:id", event["route"])
	}
	if _, exists := event["query"]; exists {
		t.Fatal("query must not be logged")
	}
	if _, exists := event["ip"]; exists {
		t.Fatal("client IP must not be logged")
	}
}
