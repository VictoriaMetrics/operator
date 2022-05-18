package limiter

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	"sync"
	"time"
)

// RateLimiter limits reconcile callback calls
type RateLimiter struct {
	mu        sync.Mutex
	budget    int64
	limit     int64
	deadline  time.Time
	throttled prometheus.Counter
}

// NewRateLimiter returns limiter with limit per 2 seconds
func NewRateLimiter(limiterName string, limit int64) *RateLimiter {
	collector := prometheus.NewCounter(prometheus.CounterOpts{
		Name:        "operator_reconcile_throttled_events_total",
		Help:        "some useless help",
		ConstLabels: map[string]string{"reconciler": limiterName},
	})
	r := metrics.Registry
	r.MustRegister(collector)
	return &RateLimiter{
		limit:     limit,
		throttled: collector,
	}
}

// MustThrottleReconcile registers reconcile event and checks if it must be throttled
func (rt *RateLimiter) MustThrottleReconcile() bool {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.budget <= 0 {
		if d := time.Until(rt.deadline); d > 0 {
			rt.throttled.Inc()
			return true
		}
		rt.deadline = time.Now().Add(time.Second * 2)
		rt.budget += rt.limit
	}
	rt.budget--
	return false
}
