// Package metrics counts what the API does and serves it in the Prometheus text format on a listener of its own. The listener
// is never the public one, so what it shows (route names, error rates, pool sizes) cannot be read from the internet, and a
// scraper reaches it only through the private network of the deployment.
package metrics

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"

	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
)

// Unknown is the label of a request whose route or method is not one the API registered. A label that came from the client
// would let anyone fill the memory of the process and of the scraper with series, so only names known at start-up are used.
const Unknown = "unmatched"

// rateLimitedCode is the code of every 429 that a limiter or a busy password pool gives
const rateLimitedCode = "RATE_LIMITED"

// Metrics holds the collectors and the registry that the listener serves
type Metrics struct {
	registry *prometheus.Registry

	requests   *prometheus.CounterVec
	duration   *prometheus.HistogramVec
	inFlight   prometheus.Gauge
	limited    *prometheus.CounterVec
	hashBusy   prometheus.Counter
	jobRuns    *prometheus.CounterVec
	jobSuccess *prometheus.GaugeVec

	// routes is every route the API registered, set once before the first request and only read after it
	routes map[string]struct{}
	now    func() time.Time
}

// methods are the HTTP methods that get a label of their own, any other one is counted as OTHER
var methods = map[string]struct{}{
	fiber.MethodGet: {}, fiber.MethodPost: {}, fiber.MethodPut: {}, fiber.MethodPatch: {}, fiber.MethodDelete: {},
	fiber.MethodHead: {}, fiber.MethodOptions: {},
}

// New builds the collectors on a registry of their own, next to the ones of the Go runtime and of the process
func New() *Metrics {
	m := &Metrics{
		registry: prometheus.NewRegistry(),
		routes:   map[string]struct{}{},
		now:      time.Now,
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "kibooz", Name: "http_requests_total", Help: "Requests answered, by method, route and status code.",
		}, []string{"method", "route", "status"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "kibooz", Name: "http_request_duration_seconds", Help: "Time to answer a request, by method and route.",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		}, []string{"method", "route"}),
		inFlight: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "kibooz", Name: "http_requests_in_flight", Help: "Requests that are being answered now.",
		}),
		limited: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "kibooz", Name: "rate_limited_total", Help: "Requests refused by a rate limiter, by route.",
		}, []string{"route"}),
		hashBusy: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "kibooz", Name: "password_hash_busy_total", Help: "Requests refused because every password hashing slot was taken.",
		}),
		jobRuns: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "kibooz", Name: "job_runs_total", Help: "Runs of a background job, by job and result.",
		}, []string{"job", "result"}),
		jobSuccess: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "kibooz", Name: "job_last_success_timestamp_seconds", Help: "Unix time of the last successful run of a background job.",
		}, []string{"job"}),
	}

	m.registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		m.requests, m.duration, m.inFlight, m.limited, m.hashBusy, m.jobRuns, m.jobSuccess,
	)

	return m
}

// Registry is where more collectors are added, such as the statistics of the database pool
func (m *Metrics) Registry() *prometheus.Registry { return m.registry }

// AllowRoutes names the routes that may appear as a label. It is called once, after every route is registered.
func (m *Metrics) AllowRoutes(routes []fiber.Route) {
	for _, route := range routes {
		m.routes[route.Path] = struct{}{}
	}
}

// Middleware measures every request that reaches it. It sits inside the access log and answers an error itself, with the
// error handler of the app, so the status it counts is the one the client gets.
func (m *Metrics) Middleware() fiber.Handler {
	return func(c fiber.Ctx) error {
		start := m.now()

		m.inFlight.Inc()

		// a panic leaves through the recover middleware, which answers 500, and has no status to read here
		finished := false

		defer func() {
			m.inFlight.Dec()

			if !finished {
				m.record(c, http.StatusInternalServerError, start)
			}
		}()

		err := c.Next()
		if err != nil {
			if handled := c.App().ErrorHandler(c, err); handled != nil {
				_ = c.SendStatus(http.StatusInternalServerError)
			}

			m.countRefusal(c, err)
		}

		finished = true

		m.record(c, c.Response().StatusCode(), start)

		return nil
	}
}

func (m *Metrics) record(c fiber.Ctx, status int, start time.Time) {
	method := "OTHER"
	if _, ok := methods[c.Method()]; ok {
		method = c.Method()
	}

	route := m.routeOf(c)

	m.requests.WithLabelValues(method, route, strconv.Itoa(status)).Inc()
	m.duration.WithLabelValues(method, route).Observe(m.now().Sub(start).Seconds())
}

// routeOf is the path that a route was registered with, so /api/v1/wali/children/{id} is one series and not one for every id
func (m *Metrics) routeOf(c fiber.Ctx) string {
	route := c.Route()
	if route == nil {
		return Unknown
	}

	if _, ok := m.routes[route.Path]; !ok {
		return Unknown
	}

	return route.Path
}

// countRefusal counts the 429 of a limiter and the 429 of a busy password pool, which have the same code on purpose
func (m *Metrics) countRefusal(c fiber.Ctx, err error) {
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != rateLimitedCode {
		return
	}

	if appErr == apperror.ErrBusy {
		m.hashBusy.Inc()

		return
	}

	m.limited.WithLabelValues(m.routeOf(c)).Inc()
}

// Job returns what a background job calls after every run with the error of the run, nil for a success. A run that was
// cut short by the shutdown is the job's to leave out.
func (m *Metrics) Job(name string) func(error) {
	// the series exist from the start, so a job that never ran still shows and an alert on its age can tell
	m.jobRuns.WithLabelValues(name, "success")
	m.jobRuns.WithLabelValues(name, "failure")

	return func(err error) {
		if err != nil {
			m.jobRuns.WithLabelValues(name, "failure").Inc()

			return
		}

		m.jobRuns.WithLabelValues(name, "success").Inc()
		m.jobSuccess.WithLabelValues(name).Set(float64(m.now().Unix()))
	}
}
