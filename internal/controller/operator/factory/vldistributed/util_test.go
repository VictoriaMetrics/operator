package vldistributed

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"k8s.io/utils/ptr"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/podutil"
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

		values, err := podutil.FetchMetricValues(ctx, ts.Client(), ts.URL, metric, dimension)
		assert.NoError(t, err)

		assert.Equal(t, expected, values)
	}

	// test metric
	f(`
test_metric{url="http://example.com"} 24
`, "test_metric", "url", map[string]float64{
		"http://example.com": 24,
	})

	// ignores similarly prefixed metrics
	f(`
test_metric_total{url="http://wrong.com"} 99
test_metric{url="http://correct.com"} 24
`, "test_metric", "url", map[string]float64{
		"http://correct.com": 24,
	})

	// ignores prometheus metadata lines
	f(`
# HELP test_metric queue size
# TYPE test_metric gauge
test_metric{url="http://correct.com"} 24
`, "test_metric", "url", map[string]float64{
		"http://correct.com": 24,
	})
}

func TestMergeSpecs(t *testing.T) {
	t.Run("VLCluster", func(t *testing.T) {
		f := func(common, zone *vmv1.VLClusterSpec, zoneName string, expected *vmv1.VLClusterSpec) {
			t.Helper()
			got, err := podutil.MergeSpecs(common, zone, zoneName)
			assert.NoError(t, err)
			assert.Equal(t, expected, got)
		}

		// zone-specific cluster version
		f(&vmv1.VLClusterSpec{
			ClusterVersion: "v1.51.0",
			VLStorage: &vmv1.VLStorage{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To[int32](1),
				},
			},
		}, &vmv1.VLClusterSpec{
			ClusterVersion: "v2.0.0",
		}, "zone-a", &vmv1.VLClusterSpec{
			ClusterVersion: "v2.0.0",
			VLStorage: &vmv1.VLStorage{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To[int32](1),
				},
			},
		})

		// %ZONE% templating
		f(&vmv1.VLClusterSpec{
			ClusterVersion: "v1.51.0",
			VLStorage: &vmv1.VLStorage{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To[int32](1),
					NodeSelector: map[string]string{
						"topology.kubernetes.io/zone": "%ZONE%",
					},
				},
			},
		}, &vmv1.VLClusterSpec{
			ClusterVersion: "v2.0.0",
		}, "zone-a", &vmv1.VLClusterSpec{
			ClusterVersion: "v2.0.0",
			VLStorage: &vmv1.VLStorage{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To[int32](1),
					NodeSelector: map[string]string{
						"topology.kubernetes.io/zone": "zone-a",
					},
				},
			},
		})
	})

	t.Run("VLAgent", func(t *testing.T) {
		f := func(common, zone *vmv1alpha1.VLDistributedZoneAgentSpec, zoneName string, expected *vmv1alpha1.VLDistributedZoneAgentSpec) {
			t.Helper()
			got, err := podutil.MergeSpecs(common, zone, zoneName)
			assert.NoError(t, err)
			assert.Equal(t, expected, got)
		}

		f(&vmv1alpha1.VLDistributedZoneAgentSpec{
			CommonAppsParams: vmv1beta1.CommonAppsParams{
				NodeSelector: map[string]string{
					"topology.kubernetes.io/zone": "%ZONE%",
				},
			},
		}, &vmv1alpha1.VLDistributedZoneAgentSpec{}, "zone-b", &vmv1alpha1.VLDistributedZoneAgentSpec{
			CommonAppsParams: vmv1beta1.CommonAppsParams{
				NodeSelector: map[string]string{
					"topology.kubernetes.io/zone": "zone-b",
				},
			},
		})
	})
}
