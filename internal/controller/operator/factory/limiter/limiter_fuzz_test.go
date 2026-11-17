package limiter

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func FuzzThrottle(f *testing.F) {
	f.Add(int64(5), int64(time.Second), []byte{0, 0, 0, 0, 0, 0})
	f.Add(int64(0), int64(0), []byte{1, 2, 3})
	f.Add(int64(-1), int64(-time.Second), []byte{255, 0, 255})
	f.Add(int64(2), int64(10*time.Millisecond), []byte{0, 0, 0, 20, 0, 0})
	f.Fuzz(func(t *testing.T, limit, duration int64, sleeps []byte) {
		// fake clock makes sleeps instant
		synctest.Test(t, func(t *testing.T) {
			rl := &ReconcileRateLimiter{
				RateLimiter: *NewRateLimiter(limit, time.Duration(duration)),
				throttled:   prometheus.NewCounter(prometheus.CounterOpts{Name: "fuzz"}),
			}
			for _, s := range sleeps {
				time.Sleep(time.Duration(s) * time.Millisecond)
				_ = rl.Throttle()
			}
		})
	})
}
