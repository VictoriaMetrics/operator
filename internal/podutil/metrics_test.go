package podutil

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestFetchMetricValues(t *testing.T) {
	f := func(body, metric, dimension string, expected map[string]float64) {
		t.Helper()
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintln(w, body)
		}))
		defer ts.Close()

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		values, err := FetchMetricValues(ctx, ts.Client(), ts.URL, metric, dimension)
		assert.NoError(t, err)

		assert.Equal(t, expected, values)
	}

	// test metric
	f(`
test_metric{path="no path"} 24
`, "test_metric", "path", map[string]float64{
		"no path": 24,
	})

	// ignores similarly prefixed metrics
	f(`
test_metric_total{path="wrong"} 99
test_metric{path="correct"} 24
`, "test_metric", "path", map[string]float64{
		"correct": 24,
	})

	// ignores prometheus metadata lines
	f(`
# HELP test_metric queue size
# TYPE test_metric gauge
test_metric{path="correct"} 24
`, "test_metric", "path", map[string]float64{
		"correct": 24,
	})
}
