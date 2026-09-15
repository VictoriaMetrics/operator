package manager

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/stretchr/testify/require"

	"github.com/VictoriaMetrics/operator/internal/podutil"
)

// overlappingProcessMetrics: labelless go_*/process_* metrics emitted by both controller-runtime's default collectors and vmmetrics.WritePrometheus; each must appear exactly once per scrape.
var overlappingProcessMetrics = []string{
	"go_goroutines",
	"go_threads",
	"go_info",
	"process_cpu_seconds_total",
	"process_resident_memory_bytes",
	"process_virtual_memory_bytes",
	"process_start_time_seconds",
}

// vmOnlyMetrics: names emitted only by vmmetrics.WritePrometheus, asserting VM enrichment actually ran, not just that nothing is duplicated.
var vmOnlyMetrics = []string{
	"process_io_read_bytes_total",
	"process_cpu_seconds_system_total",
	"go_cpu_count",
	"go_info_ext",
}

// TestMustDropDefaultProcessMetrics guards against duplicate go_*/process_* metrics on "/metrics" and confirms VM's own metrics are still present.
func TestMustDropDefaultProcessMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	// mimic sigs.k8s.io/controller-runtime/pkg/internal/controller/metrics.init()
	reg.MustRegister(
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		collectors.NewGoCollector(collectors.WithGoCollectorRuntimeMetrics(collectors.MetricsAll)),
	)

	dropDefaultProcessMetrics(reg)

	filter, err := vmMetricsFilterProvider(nil, nil)
	require.NoError(t, err)
	handler, err := filter(logr.Discard(), promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	require.NoError(t, err)

	srv := httptest.NewServer(handler)
	defer srv.Close()

	// empty Dimension requests every sample regardless of labels, so a duplicate surfaces as len(values[name]) > 1.
	queries := make([]podutil.MetricQuery, 0, len(overlappingProcessMetrics)+len(vmOnlyMetrics))
	for _, name := range overlappingProcessMetrics {
		queries = append(queries, podutil.MetricQuery{Name: name})
	}
	for _, name := range vmOnlyMetrics {
		queries = append(queries, podutil.MetricQuery{Name: name})
	}

	values, err := podutil.FetchMetricsValues(context.Background(), srv.Client(), srv.URL, queries)
	require.NoError(t, err)

	for _, name := range overlappingProcessMetrics {
		require.Lenf(t, values[name], 1, "metric %q is duplicated on /metrics", name)
	}
	for _, name := range vmOnlyMetrics {
		require.NotEmptyf(t, values[name], "expected VM metric %q missing from /metrics", name)
	}
}
