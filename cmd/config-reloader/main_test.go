package main

import (
	"context"
	"flag"
	"sync/atomic"
	"testing"
	"time"
)

func TestLogFormatAlias(t *testing.T) {
	f := func(logFormatVal, loggerFormatVal, expected string) {
		t.Helper()
		_ = flag.Set("log-format", logFormatVal)
		_ = flag.Set("loggerFormat", loggerFormatVal)

		if *logFormat != "" {
			_ = flag.Set("loggerFormat", *logFormat)
		}

		loggerFormatFlag := flag.Lookup("loggerFormat")
		if loggerFormatFlag == nil {
			t.Fatalf("expected loggerFormat flag to be registered")
		}

		if loggerFormatFlag.Value.String() != expected {
			t.Fatalf("expected loggerFormat to be %q, got %q", expected, loggerFormatFlag.Value.String())
		}
	}

	// only log-format is set
	f("json", "default", "json")

	// log-format is empty
	f("", "json", "json")
}

// TestCfgWatcherSignalSentOnce verifies that a burst of updates results in
// exactly one reloader call (channel drained before reload).
func TestCfgWatcherSignalSentOnce(t *testing.T) {
	origDelay := *delayInterval
	*delayInterval = 0
	defer func() { *delayInterval = origDelay }()

	var reloadCount atomic.Int64
	updates := make(chan struct{}, 10)
	w := cfgWatcher{
		updates: updates,
		reloader: func(_ context.Context) error {
			reloadCount.Add(1)
			return nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.start(ctx)

	for range 5 {
		select {
		case updates <- struct{}{}:
		default:
		}
	}

	time.Sleep(100 * time.Millisecond)
	cancel()
	w.close()

	if got := reloadCount.Load(); got != 1 {
		t.Fatalf("expected 1 reload call, got %d", got)
	}
}

// TestCfgWatcherDelayIntervalHonoured verifies that the reloader is not called
// before delayInterval elapses after an update signal.
func TestCfgWatcherDelayIntervalHonoured(t *testing.T) {
	delay := 150 * time.Millisecond
	origDelay := *delayInterval
	*delayInterval = delay
	defer func() { *delayInterval = origDelay }()

	var reloadCount atomic.Int64
	updates := make(chan struct{}, 10)
	w := cfgWatcher{
		updates: updates,
		reloader: func(_ context.Context) error {
			reloadCount.Add(1)
			return nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.start(ctx)

	updates <- struct{}{}

	// before delay elapses - no reload yet
	time.Sleep(50 * time.Millisecond)
	if got := reloadCount.Load(); got != 0 {
		t.Fatalf("expected 0 reload calls before delay, got %d", got)
	}

	// after delay elapses - exactly one reload
	time.Sleep(200 * time.Millisecond)
	if got := reloadCount.Load(); got != 1 {
		t.Fatalf("expected 1 reload call after delay, got %d", got)
	}
}

// TestCfgWatcherDelayIntervalDebouncesUpdates verifies that multiple updates
// arriving within the delay window are coalesced into a single reload call.
func TestCfgWatcherDelayIntervalDebouncesUpdates(t *testing.T) {
	delay := 150 * time.Millisecond
	origDelay := *delayInterval
	*delayInterval = delay
	defer func() { *delayInterval = origDelay }()

	var reloadCount atomic.Int64
	updates := make(chan struct{}, 10)
	w := cfgWatcher{
		updates: updates,
		reloader: func(_ context.Context) error {
			reloadCount.Add(1)
			return nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.start(ctx)

	// send first signal then more signals before delay elapses
	updates <- struct{}{}
	time.Sleep(20 * time.Millisecond)
	for range 4 {
		select {
		case updates <- struct{}{}:
		default:
		}
	}

	// wait for delay + processing
	time.Sleep(300 * time.Millisecond)
	cancel()
	w.close()

	if got := reloadCount.Load(); got != 1 {
		t.Fatalf("expected 1 reload call for burst within delay window, got %d", got)
	}
}

// TestCfgWatcherDelayIntervalCancelledContext verifies that cancelling context
// during delay window prevents reloader from being called.
func TestCfgWatcherDelayIntervalCancelledContext(t *testing.T) {
	delay := 500 * time.Millisecond
	origDelay := *delayInterval
	*delayInterval = delay
	defer func() { *delayInterval = origDelay }()

	var reloadCount atomic.Int64
	updates := make(chan struct{}, 10)
	w := cfgWatcher{
		updates: updates,
		reloader: func(_ context.Context) error {
			reloadCount.Add(1)
			return nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	w.start(ctx)

	updates <- struct{}{}

	// cancel before delay elapses
	time.Sleep(50 * time.Millisecond)
	cancel()
	w.close()

	if got := reloadCount.Load(); got != 0 {
		t.Fatalf("expected 0 reload calls after context cancel, got %d", got)
	}
}
