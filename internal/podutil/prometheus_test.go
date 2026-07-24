package podutil

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestFetchMetricsValues(t *testing.T) {
	f := func(body string, queries []MetricQuery, expected map[string]map[string]float64) {
		t.Helper()
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintln(w, body)
		}))
		defer ts.Close()

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		values, err := FetchMetricsValues(ctx, ts.Client(), ts.URL, queries)
		assert.NoError(t, err)

		assert.Equal(t, expected, values)
	}

	// test metric
	f(`
test_metric{path="no path"} 24
`, []MetricQuery{{Name: "test_metric", Dimension: "path"}}, map[string]map[string]float64{
		"test_metric": {"no path": 24},
	})

	// ignores similarly prefixed metrics
	f(`
test_metric_total{path="wrong"} 99
test_metric{path="correct"} 24
`, []MetricQuery{{Name: "test_metric", Dimension: "path"}}, map[string]map[string]float64{
		"test_metric": {"correct": 24},
	})

	// ignores prometheus metadata lines
	f(`
# HELP test_metric queue size
# TYPE test_metric gauge
test_metric{path="correct"} 24
`, []MetricQuery{{Name: "test_metric", Dimension: "path"}}, map[string]map[string]float64{
		"test_metric": {"correct": 24},
	})

	// empty dimension returns every sample regardless of labels
	f(`
test_metric 24
`, []MetricQuery{{Name: "test_metric"}}, map[string]map[string]float64{
		"test_metric": {"0": 24},
	})

	// multiple queries are resolved from a single scrape
	f(`
metric_a 1
metric_b{path="x"} 2
`, []MetricQuery{{Name: "metric_a"}, {Name: "metric_b", Dimension: "path"}}, map[string]map[string]float64{
		"metric_a": {"0": 1},
		"metric_b": {"x": 2},
	})
}

func TestFetchMetricsValuesLargeResponse(t *testing.T) {
	var body strings.Builder
	for range 200_000 {
		body.WriteString("unrelated_metric{label=\"x\"} 1\n")
	}
	body.WriteString(`test_metric{path="correct"} 24` + "\n")

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, body.String())
	}))
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	values, err := FetchMetricsValues(ctx, ts.Client(), ts.URL, []MetricQuery{{Name: "test_metric", Dimension: "path"}})
	assert.NoError(t, err)
	assert.Equal(t, map[string]map[string]float64{"test_metric": {"correct": 24}}, values)
}

func TestFetchMetricsValuesLineTooLong(t *testing.T) {
	line := `test_metric{label="` + strings.Repeat("x", maxMetricLineBytes) + `"} 24`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, line+"\n")
	}))
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := FetchMetricsValues(ctx, ts.Client(), ts.URL, []MetricQuery{{Name: "test_metric", Dimension: "label"}})
	assert.Error(t, err)
}
