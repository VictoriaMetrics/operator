package main

import (
	"bytes"
	"fmt"
	"hash/crc32"
	"strings"
	"testing"

	"github.com/VictoriaMetrics/metrics"
)

func TestContentHashRegistry_ConfirmReload(t *testing.T) {
	set := metrics.NewSet()
	r := newContentHashRegistry(set)

	// observing without confirming must not publish anything yet.
	r.observe("main", hashBytes([]byte("config-v1")))
	if got := scrapeGauge(t, set, "main"); got != nil {
		t.Fatalf("expected no published hash before confirmReload, got %v", *got)
	}

	r.confirmReload()
	want := float64(crc32.ChecksumIEEE([]byte("config-v1")))
	got := scrapeGauge(t, set, "main")
	if got == nil || *got != want {
		t.Fatalf("expected published hash %v, got %v", want, got)
	}

	// a superseding observe() must not change the published value until confirmed again.
	r.observe("main", hashBytes([]byte("config-v2")))
	got = scrapeGauge(t, set, "main")
	if got == nil || *got != want {
		t.Fatalf("expected published hash to remain %v before reconfirm, got %v", want, got)
	}

	r.confirmReload()
	want2 := float64(crc32.ChecksumIEEE([]byte("config-v2")))
	got = scrapeGauge(t, set, "main")
	if got == nil || *got != want2 {
		t.Fatalf("expected published hash %v after reconfirm, got %v", want2, got)
	}
}

func TestContentHashRegistry_KeysAreIndependent(t *testing.T) {
	set := metrics.NewSet()
	r := newContentHashRegistry(set)
	r.observe("main", hashBytes([]byte("main-content")))
	r.observe("/etc/vmagent/relabel", hashBytes([]byte("relabel-content")))
	r.confirmReload()

	mainWant := float64(crc32.ChecksumIEEE([]byte("main-content")))
	relabelWant := float64(crc32.ChecksumIEEE([]byte("relabel-content")))

	if got := scrapeGauge(t, set, "main"); got == nil || *got != mainWant {
		t.Fatalf("expected main hash %v, got %v", mainWant, got)
	}
	if got := scrapeGauge(t, set, "/etc/vmagent/relabel"); got == nil || *got != relabelWant {
		t.Fatalf("expected relabel hash %v, got %v", relabelWant, got)
	}
}

// scrapeGauge renders set and returns the value of the sample matching
// configreloader_reload_content_hash{key="key"}, or nil if no such sample was written.
func scrapeGauge(t *testing.T, set *metrics.Set, key string) *float64 {
	t.Helper()
	var buf bytes.Buffer
	set.WritePrometheus(&buf)
	prefix := `configreloader_reload_content_hash{key="` + key + `"} `
	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.HasPrefix(line, prefix) {
			var v float64
			if _, err := fmt.Sscan(line[len(prefix):], &v); err != nil {
				t.Fatalf("cannot parse metric value from line %q: %s", line, err)
			}
			return &v
		}
	}
	return nil
}
