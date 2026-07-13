package main

import (
	"fmt"
	"hash/crc32"
	"sync"

	"github.com/VictoriaMetrics/metrics"
)

// contentHashRegistry exposes, per watched source key, the CRC32 of the content last
// confirmed applied by a successful reload - never the merely-observed pending value, so a
// scraper can't see a hash for content the target app hasn't actually accepted via /-/reload.
type contentHashRegistry struct {
	mu        sync.Mutex
	set       *metrics.Set
	pending   map[string]uint32
	confirmed map[string]uint32
}

func newContentHashRegistry(set *metrics.Set) *contentHashRegistry {
	return &contentHashRegistry{
		set:       set,
		pending:   make(map[string]uint32),
		confirmed: make(map[string]uint32),
	}
}

func (r *contentHashRegistry) observe(key string, hash uint32) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pending[key] = hash
}

func hashBytes(data []byte) uint32 {
	return crc32.ChecksumIEEE(data)
}

// confirmReload publishes every pending hash as confirmed. Called only after a successful
// (HTTP 200/204) /-/reload call.
func (r *contentHashRegistry) confirmReload() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, hash := range r.pending {
		if _, exists := r.confirmed[key]; !exists {
			r.registerMetricLocked(key)
		}
		r.confirmed[key] = hash
	}
}

// registerMetricLocked must be called with r.mu held.
func (r *contentHashRegistry) registerMetricLocked(key string) {
	name := fmt.Sprintf(`configreloader_reload_content_hash{key=%q}`, key)
	r.set.GetOrCreateGauge(name, func() float64 {
		r.mu.Lock()
		defer r.mu.Unlock()
		return float64(r.confirmed[key])
	})
}

var contentHashes = newContentHashRegistry(metrics.GetDefaultSet())
