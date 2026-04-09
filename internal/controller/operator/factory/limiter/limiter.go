package limiter

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

// NewRateLimiter returns limiter with limit per duration
func NewRateLimiter(limit int64, duration time.Duration) *RateLimiter {
	return &RateLimiter{
		limit:    limit,
		duration: duration,
	}
}

// RateLimiter limits callback calls
type RateLimiter struct {
	budget   int64
	limit    int64
	duration time.Duration
	deadline time.Time
}

// Throttle registers reconcile event and checks if it must be throttled
func (rt *RateLimiter) Throttle() bool {
	if rt.budget <= 0 {
		if d := time.Until(rt.deadline); d > 0 {
			return true
		}
		rt.deadline = time.Now().Add(rt.duration)
		rt.budget += rt.limit
	}
	rt.budget--
	return false
}

// NewReconcileRateLimiter returns limiter with limit per 2 seconds
func NewReconcileRateLimiter(limiterName string, limit int64) *ReconcileRateLimiter {
	collector := prometheus.NewCounter(prometheus.CounterOpts{
		Name:        "operator_reconcile_throttled_events_total",
		Help:        "number of throttled reconciliation events",
		ConstLabels: map[string]string{"controller": limiterName},
	})
	metrics.Registry.MustRegister(collector)
	return &ReconcileRateLimiter{
		RateLimiter: RateLimiter{
			limit:    limit,
			duration: time.Second * 2,
		},
		throttled: collector,
	}
}

// ReconcileRateLimiter limits reconcile callback calls
type ReconcileRateLimiter struct {
	RateLimiter
	mu        sync.Mutex
	throttled prometheus.Counter
}

// Throttle registers reconcile event and checks if it must be throttled
func (rt *ReconcileRateLimiter) Throttle() bool {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.RateLimiter.Throttle() {
		rt.throttled.Inc()
		return true
	}
	return false
}
