package limiter

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRateLimiter_Throttle(t *testing.T) {
	f := func(limit int64, duration time.Duration, calls int, wantThrottled int) {
		t.Helper()
		rl := NewRateLimiter(limit, duration)
		throttled := 0
		for i := 0; i < calls; i++ {
			if rl.Throttle() {
				throttled++
			}
		}
		assert.Equal(t, wantThrottled, throttled)
	}

	// happy path: 5 calls
	f(5, time.Second, 5, 0)

	// happy path: one call
	f(1, time.Second, 1, 0)

	// 3 calls over budget: 3 get throttled
	f(3, time.Second, 6, 3)

	// zero budget: first call succeeds, rest throttled
	f(0, time.Second, 3, 2)
}

func TestRateLimiter_ThrottleResetsAfterDeadline(t *testing.T) {
	rl := NewRateLimiter(2, 10*time.Millisecond)

	// exhaust the budget
	assert.False(t, rl.Throttle())
	assert.False(t, rl.Throttle())
	// throttling begins
	assert.True(t, rl.Throttle())

	// budget resets after deadline
	time.Sleep(20 * time.Millisecond)
	assert.False(t, rl.Throttle())
	assert.False(t, rl.Throttle())
	// throttling begins
	assert.True(t, rl.Throttle())
}

func TestReconcileRateLimiter_Throttle(t *testing.T) {
	f := func(limit int64, calls int, wantThrottled int) {
		t.Helper()
		// create ReconcileRateLimiter directly to avoid prometheus registration
		rl := &ReconcileRateLimiter{
			RateLimiter: RateLimiter{
				limit:    limit,
				duration: time.Second,
			},
		}
		throttled := 0
		for i := 0; i < calls; i++ {
			rl.mu.Lock()
			got := rl.RateLimiter.Throttle()
			rl.mu.Unlock()
			if got {
				throttled++
			}
		}
		assert.Equal(t, wantThrottled, throttled)
	}

	// happy path
	f(5, 5, 0)

	// zero budget: first call succeeds, rest throttled
	f(0, 3, 2)

	// partial throttle
	f(2, 5, 3)
}

func TestReconcileRateLimiter_Concurrent(t *testing.T) {
	rl := &ReconcileRateLimiter{
		RateLimiter: RateLimiter{
			limit:    10,
			duration: time.Second,
		},
	}

	done := make(chan bool, 20)
	for i := 0; i < 20; i++ {
		go func() {
			rl.mu.Lock()
			rl.RateLimiter.Throttle()
			rl.mu.Unlock()
			done <- true
		}()
	}
	for i := 0; i < 20; i++ {
		<-done
	}
	// rate limited doesn't panic or race on negative budget
	assert.LessOrEqual(t, rl.budget, int64(0))
}
