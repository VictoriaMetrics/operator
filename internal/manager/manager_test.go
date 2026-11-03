package manager

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/util/sets"

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

// TestVMMetricsHandlerMerge guards against duplicate or lost go_*/process_* metrics on "/metrics" and confirms VM's own metrics are present.
func TestVMMetricsHandlerMerge(t *testing.T) {
	reg := prometheus.NewRegistry()
	// mimic sigs.k8s.io/controller-runtime/pkg/internal/controller/metrics.init()
	reg.MustRegister(
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		collectors.NewGoCollector(collectors.WithGoCollectorRuntimeMetrics(collectors.MetricsAll)),
	)
	families, err := reg.Gather()
	require.NoError(t, err)

	// histogram/summary families only appear as <name>_count etc. in text exposition
	var defaults []string
	for _, mf := range families {
		name := mf.GetName()
		if mf.GetType() == dto.MetricType_HISTOGRAM || mf.GetType() == dto.MetricType_SUMMARY {
			name += "_count"
		}
		defaults = append(defaults, name)
	}

	srv := httptest.NewServer(vmMetricsHandler(reg))
	defer srv.Close()

	// empty Dimension requests every sample regardless of labels, so a duplicate surfaces as len(values[name]) > 1.
	queries := make([]podutil.MetricQuery, 0, len(overlappingProcessMetrics)+len(vmOnlyMetrics))
	for _, name := range overlappingProcessMetrics {
		queries = append(queries, podutil.MetricQuery{Name: name})
	}
	for _, name := range vmOnlyMetrics {
		queries = append(queries, podutil.MetricQuery{Name: name})
	}
	for _, name := range defaults {
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
	for _, name := range defaults {
		require.Lenf(t, values[name], 1, "default metric %q missing or duplicated on /metrics", name)
	}
}

// TestVMMetricsHandlerProtobuf guards against VM metrics being dropped or corrupting non-text scrapes.
func TestVMMetricsHandlerProtobuf(t *testing.T) {
	checkVMMetricsFormat(t, "application/vnd.google.protobuf;proto=io.prometheus.client.MetricFamily;encoding=delimited", expfmt.TypeProtoDelim)
}

// TestVMMetricsHandlerText guards against VM metrics being dropped or corrupting text scrapes.
func TestVMMetricsHandlerText(t *testing.T) {
	checkVMMetricsFormat(t, "text/plain;version=0.0.4", expfmt.TypeTextPlain)
}

func checkVMMetricsFormat(t *testing.T, accept string, want expfmt.FormatType) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Accept", accept)
	rec := httptest.NewRecorder()
	vmMetricsHandler(prometheus.NewRegistry()).ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	format := expfmt.ResponseFormat(rec.Header())
	require.Equal(t, want, format.FormatType())

	// decode every family; a corrupted stream fails here
	names := sets.New[string]()
	dec := expfmt.NewDecoder(rec.Body, format)
	for {
		var mf dto.MetricFamily
		err := dec.Decode(&mf)
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		names.Insert(mf.GetName())
	}
	for _, name := range vmOnlyMetrics {
		require.Truef(t, names.Has(name), "expected VM metric %q missing from %s /metrics", name, want)
	}
}
