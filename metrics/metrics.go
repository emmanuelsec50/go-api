package metrics

import (
	"context"
	"errors"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/redis/go-redis/v9"
)

// HTTP metrics.
var (
	httpRPS = newEWMARate(time.Second)

	httpRequestsPerSecond = promauto.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "http_requests_per_second",
		Help: "Exponentially weighted moving average of HTTP requests per second (1 second time constant).",
	}, httpRPS.get)

	httpRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Total number of HTTP requests handled.",
	}, []string{"method", "route", "status"})

	httpRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "HTTP request latency in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "route", "status"})

	httpRequestsInFlight = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "http_requests_in_flight",
		Help: "Number of HTTP requests currently being handled.",
	})
)

// Business metrics.
var (
	ordersRPS = newEWMARate(time.Second)

	ordersCreatedPerSecond = promauto.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "orders_created_per_second",
		Help: "Exponentially weighted moving average of orders created per second (1 second time constant).",
	}, ordersRPS.get)

	ordersCreated = promauto.NewCounter(prometheus.CounterOpts{
		Name: "orders_created_total",
		Help: "Total number of orders created.",
	})

	ordersCreateErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "orders_create_errors_total",
		Help: "Total number of order creation attempts that failed.",
	})

	ordersListed = promauto.NewCounter(prometheus.CounterOpts{
		Name: "orders_listed_total",
		Help: "Total number of list orders requests handled successfully.",
	})

	ordersLookup = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "orders_lookup_total",
		Help: "Total number of order lookups by result.",
	}, []string{"result"})

	ordersUpdated = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "orders_updated_total",
		Help: "Total number of orders updated by new status.",
	}, []string{"status"})

	ordersDeleted = promauto.NewCounter(prometheus.CounterOpts{
		Name: "orders_deleted_total",
		Help: "Total number of orders deleted.",
	})
)

// Redis command metrics.
var (
	redisCommandDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "redis_command_duration_seconds",
		Help:    "Redis command latency in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"command"})

	redisCommandErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "redis_command_errors_total",
		Help: "Total number of Redis commands that failed.",
	}, []string{"command"})
)

// HTTPMiddleware records request count, duration, and in-flight requests.
func HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		httpRequestsInFlight.Inc()
		defer httpRequestsInFlight.Dec()

		ww := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(ww, r)

		status := strconv.Itoa(ww.status)
		httpRequestsTotal.WithLabelValues(r.Method, routePattern(r), status).Inc()
		httpRequestDuration.WithLabelValues(r.Method, routePattern(r), status).Observe(time.Since(start).Seconds())
		httpRPS.update()
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func routePattern(r *http.Request) string {
	if rctx := chi.RouteContext(r.Context()); rctx != nil && rctx.RoutePattern() != "" {
		return rctx.RoutePattern()
	}
	return "unknown"
}

// RedisHook records Redis command latency and errors.
type RedisHook struct{}

func (RedisHook) DialHook(next redis.DialHook) redis.DialHook { return next }

func (RedisHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		start := time.Now()
		err := next(ctx, cmd)
		recordRedisCommand(cmd.Name(), start, err)
		return err
	}
}

func (RedisHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		start := time.Now()
		err := next(ctx, cmds)
		recordRedisCommand("pipeline", start, err)
		return err
	}
}

func recordRedisCommand(command string, start time.Time, err error) {
	redisCommandDuration.WithLabelValues(command).Observe(time.Since(start).Seconds())
	if err != nil && !errors.Is(err, redis.Nil) {
		redisCommandErrors.WithLabelValues(command).Inc()
	}
}

// Redis pool metrics.
var (
	redisPoolHits       = newPoolDesc("redis_pool_hits_total", "Number of times a free connection was found in the pool.")
	redisPoolMisses     = newPoolDesc("redis_pool_misses_total", "Number of times a new connection was created because the pool was empty.")
	redisPoolTimeouts   = newPoolDesc("redis_pool_timeouts_total", "Number of times a wait for a connection timed out.")
	redisPoolTotalConns = newPoolDesc("redis_pool_conns", "Number of connections currently in the pool.")
	redisPoolIdleConns  = newPoolDesc("redis_pool_idle_conns", "Number of idle connections in the pool.")
	redisPoolStaleConns = newPoolDesc("redis_pool_stale_conns", "Number of stale connections removed from the pool.")
)

func newPoolDesc(name, help string) *prometheus.Desc {
	return prometheus.NewDesc(name, help, nil, nil)
}

type redisPoolCollector struct {
	client *redis.Client
}

// RegisterRedisPool registers a collector that exposes Redis client pool stats.
func RegisterRedisPool(client *redis.Client) {
	prometheus.MustRegister(&redisPoolCollector{client: client})
}

func (c *redisPoolCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- redisPoolHits
	ch <- redisPoolMisses
	ch <- redisPoolTimeouts
	ch <- redisPoolTotalConns
	ch <- redisPoolIdleConns
	ch <- redisPoolStaleConns
}

func (c *redisPoolCollector) Collect(ch chan<- prometheus.Metric) {
	stats := c.client.PoolStats()
	ch <- prometheus.MustNewConstMetric(redisPoolHits, prometheus.CounterValue, float64(stats.Hits))
	ch <- prometheus.MustNewConstMetric(redisPoolMisses, prometheus.CounterValue, float64(stats.Misses))
	ch <- prometheus.MustNewConstMetric(redisPoolTimeouts, prometheus.CounterValue, float64(stats.Timeouts))
	ch <- prometheus.MustNewConstMetric(redisPoolTotalConns, prometheus.GaugeValue, float64(stats.TotalConns))
	ch <- prometheus.MustNewConstMetric(redisPoolIdleConns, prometheus.GaugeValue, float64(stats.IdleConns))
	ch <- prometheus.MustNewConstMetric(redisPoolStaleConns, prometheus.CounterValue, float64(stats.StaleConns))
}

// Business metric helpers.
func IncOrderCreated()              { ordersCreated.Inc(); ordersRPS.update() }
func IncOrderCreateError()          { ordersCreateErrors.Inc() }
func IncOrderListed()               { ordersListed.Inc() }
func IncOrderLookup(result string)  { ordersLookup.WithLabelValues(result).Inc() }
func IncOrderUpdated(status string) { ordersUpdated.WithLabelValues(status).Inc() }
func IncOrderDeleted()              { ordersDeleted.Inc() }

// ewmaRate tracks an exponentially weighted moving average of events per second.
type ewmaRate struct {
	mu    sync.Mutex
	tau   float64 // smoothing time constant in seconds
	value float64
	last  time.Time
}

func newEWMARate(tau time.Duration) *ewmaRate {
	return &ewmaRate{tau: tau.Seconds()}
}

// update records one event and recomputes the rate.
func (r *ewmaRate) update() {
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.last.IsZero() {
		r.last = now
		return
	}

	delta := now.Sub(r.last).Seconds()
	if delta <= 0 {
		return
	}

	instant := 1 / delta
	alpha := 1 - math.Exp(-delta/r.tau)
	r.value = (1-alpha)*r.value + alpha*instant
	r.last = now
}

// value returns the current rate, decaying it by the elapsed time since the last event.
func (r *ewmaRate) get() float64 {
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.last.IsZero() {
		return 0
	}

	delta := now.Sub(r.last).Seconds()
	if delta > 0 {
		r.value *= math.Exp(-delta / r.tau)
		r.last = now
	}
	return r.value
}
