package vmdistributed

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/utils/ptr"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
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

		values, err := fetchMetricValues(ctx, ts.Client(), ts.URL, metric, dimension)
		assert.NoError(t, err)

		assert.Equal(t, expected, values)
	}

	// test metric
	f(`
test_metric{path="no path"} 24
`, "test_metric", "path", map[string]float64{
		"no path": 24,
	})
}

func TestMergeSpecs(t *testing.T) {
	t.Run("VMCluster", func(t *testing.T) {
		f := func(common, zone *vmv1beta1.VMClusterSpec, zoneName string, expected *vmv1beta1.VMClusterSpec) {
			t.Helper()
			got, err := mergeSpecs(common, zone, zoneName)
			require.NoError(t, err)
			assert.Equal(t, expected, got)
		}

		// zone-specific retention period
		f(&vmv1beta1.VMClusterSpec{
			RetentionPeriod: "1d",
			VMStorage: &vmv1beta1.VMStorage{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(1)),
				},
			},
		}, &vmv1beta1.VMClusterSpec{
			RetentionPeriod: "30d",
		}, "zone-a", &vmv1beta1.VMClusterSpec{
			RetentionPeriod: "30d",
			VMStorage: &vmv1beta1.VMStorage{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(1)),
				},
			},
		})

		// %ZONE% templating
		f(&vmv1beta1.VMClusterSpec{
			RetentionPeriod: "1d",
			VMStorage: &vmv1beta1.VMStorage{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(1)),
					NodeSelector: map[string]string{
						"topology.kubernetes.io/zone": "%ZONE%",
					},
				},
			},
		}, &vmv1beta1.VMClusterSpec{
			RetentionPeriod: "30d",
		}, "zone-a", &vmv1beta1.VMClusterSpec{
			RetentionPeriod: "30d",
			VMStorage: &vmv1beta1.VMStorage{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(1)),
					NodeSelector: map[string]string{
						"topology.kubernetes.io/zone": "zone-a",
					},
				},
			},
		})
	})

	t.Run("VMAgent", func(t *testing.T) {
		f := func(common, zone *vmv1alpha1.VMDistributedZoneAgentSpec, zoneName string, expected *vmv1alpha1.VMDistributedZoneAgentSpec) {
			t.Helper()
			got, err := mergeSpecs(common, zone, zoneName)
			require.NoError(t, err)
			assert.Equal(t, expected, got)
		}

		f(&vmv1alpha1.VMDistributedZoneAgentSpec{
			CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
				NodeSelector: map[string]string{
					"topology.kubernetes.io/zone": "%ZONE%",
				},
			},
		}, &vmv1alpha1.VMDistributedZoneAgentSpec{}, "zone-b", &vmv1alpha1.VMDistributedZoneAgentSpec{
			CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
				NodeSelector: map[string]string{
					"topology.kubernetes.io/zone": "zone-a",
				},
			},
		})
	})
}
