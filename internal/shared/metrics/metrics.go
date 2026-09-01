// Package metrics owns the Prometheus registry and the HTTP instrumentation.
package metrics

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Registry bundles the collectors this service exposes.
type Registry struct {
	registry         *prometheus.Registry
	requestsTotal    *prometheus.CounterVec
	requestDuration  *prometheus.HistogramVec
	requestsInFlight prometheus.Gauge
}

// New builds a registry with the portfolio's HTTP metrics plus the standard Go
// and process collectors.
//
// A private registry is used instead of the global default so tests can build
// an isolated instance and so no third-party package can silently add series.
func New() *Registry {
	reg := prometheus.NewRegistry()

	r := &Registry{
		registry: reg,
		requestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total HTTP requests by method, route, and status.",
		}, []string{"method", "route", "status"}),
		requestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "http_request_duration_seconds",
			Help: "HTTP request latency in seconds.",
			// Explicit buckets: without them there are no percentiles, which
			// is the whole reason to record latency in the first place.
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		}, []string{"method", "route", "status"}),
		requestsInFlight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "http_requests_in_flight",
			Help: "HTTP requests currently being served.",
		}),
	}

	reg.MustRegister(
		r.requestsTotal,
		r.requestDuration,
		r.requestsInFlight,
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	return r
}

// Handler serves the Prometheus text exposition format.
func (r *Registry) Handler() gin.HandlerFunc {
	h := promhttp.HandlerFor(r.registry, promhttp.HandlerOpts{})
	return func(c *gin.Context) {
		h.ServeHTTP(c.Writer, c.Request)
	}
}

// Middleware records request count and latency.
//
// The `route` label is the matched route pattern (`/api/v1/tasks/:id`), never
// the raw URL. Using the raw path would create one time series per task id and
// blow up cardinality.
func (r *Registry) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.URL.Path == "/metrics" {
			c.Next()
			return
		}

		start := time.Now()
		r.requestsInFlight.Inc()
		defer r.requestsInFlight.Dec()

		c.Next()

		route := c.FullPath()
		if route == "" {
			// Unmatched requests share one bucket rather than each becoming a
			// new series keyed on whatever the client happened to send.
			route = "unmatched"
		}
		status := strconv.Itoa(c.Writer.Status())
		labels := prometheus.Labels{
			"method": c.Request.Method,
			"route":  route,
			"status": status,
		}

		r.requestsTotal.With(labels).Inc()
		r.requestDuration.With(labels).Observe(time.Since(start).Seconds())
	}
}

// Gatherer exposes the underlying registry for tests.
func (r *Registry) Gatherer() prometheus.Gatherer { return r.registry }
