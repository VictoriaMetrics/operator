package vmdistributed

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"k8s.io/utils/ptr"

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

func TestMergeMapsRecursive(t *testing.T) {
	base := map[string]any{
		"a": map[string]any{
			"b": "keep",
			"c": "override-me",
		},
		"d": "root-keep",
	}
	override := map[string]any{
		"a": map[string]any{
			"c": "new",
			"z": "added",
		},
		"e": "root-added",
	}
	// initial merge
	modified := mergeMapsRecursive(base, override)
	assert.True(t, modified)
	assert.Equal(t, "keep", base["a"].(map[string]any)["b"])
	assert.Equal(t, "new", base["a"].(map[string]any)["c"])
	assert.Equal(t, "added", base["a"].(map[string]any)["z"])
	assert.Equal(t, "root-keep", base["d"])
	assert.Equal(t, "root-added", base["e"])
}

func TestDeepMerge(t *testing.T) {
	type opts struct {
		override *vmv1beta1.VMClusterSpec
		validate func(base, merged *vmv1beta1.VMClusterSpec, modified bool, err error)
	}
	f := func(oss ...opts) {
		t.Helper()
		base := &vmv1beta1.VMClusterSpec{
			ClusterVersion:     "v1.0.0",
			ServiceAccountName: "base",
			RetentionPeriod:    "30d",
			VMSelect: &vmv1beta1.VMSelect{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(1)),
					ExtraArgs:    map[string]string{"keep": "x", "override": "old"},
				},
			},
			VMInsert: &vmv1beta1.VMInsert{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(1)),
					ExtraArgs:    map[string]string{"insert-arg": "1"},
				},
			},
		}
		merged := base.DeepCopy()
		for _, o := range oss {
			modified, err := mergeDeep(merged, o.override)
			o.validate(base, merged, modified, err)
		}
	}

	// with extra args override
	f(opts{
		override: &vmv1beta1.VMClusterSpec{
			ClusterVersion: "v1.2.3",
			VMSelect: &vmv1beta1.VMSelect{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(3)),
					ExtraArgs:    map[string]string{"override": "new", "add": "y"},
				},
			},
			ServiceAccountName: "zone-sa",
		},
		validate: func(base, merged *vmv1beta1.VMClusterSpec, modified bool, err error) {
			assert.NoError(t, err)
			assert.True(t, modified)

			// top-level
			assert.Equal(t, "v1.2.3", merged.ClusterVersion)
			assert.Equal(t, "zone-sa", merged.ServiceAccountName)

			// nested merge
			if merged.VMSelect == nil || merged.VMSelect.ReplicaCount == nil {
				t.Fatalf("merged.VMSelect or ReplicaCount is nil")
			}
			assert.Equal(t, int32(3), *merged.VMSelect.ReplicaCount)
			assert.Equal(t, "x", merged.VMSelect.ExtraArgs["keep"])
			assert.Equal(t, "new", merged.VMSelect.ExtraArgs["override"])
			assert.Equal(t, "y", merged.VMSelect.ExtraArgs["add"])

			// untouched subtree
			if merged.VMInsert == nil || merged.VMInsert.ReplicaCount == nil {
				t.Fatalf("merged.VMInsert or ReplicaCount is nil")
			}
			assert.Equal(t, int32(1), *merged.VMInsert.ReplicaCount)
			assert.Equal(t, "1", merged.VMInsert.ExtraArgs["insert-arg"])
		},
	})

	// with nil override spec
	f(opts{
		validate: func(base, merged *vmv1beta1.VMClusterSpec, modified bool, err error) {
			assert.NoError(t, err)
			assert.False(t, modified)
			assert.Equal(t, base, merged)
		},
	})

	// with empty override spec
	f(opts{
		override: &vmv1beta1.VMClusterSpec{},
		validate: func(base, merged *vmv1beta1.VMClusterSpec, modified bool, err error) {
			assert.NoError(t, err)
			assert.False(t, modified)
			assert.Equal(t, base, merged)
		},
	})

	// with override spec that modifies top-level fields
	f(opts{
		override: &vmv1beta1.VMClusterSpec{
			ClusterVersion:     "v2.0.0",
			ServiceAccountName: "global-sa",
		},
		validate: func(_, merged *vmv1beta1.VMClusterSpec, modified bool, err error) {
			assert.True(t, modified)
			assert.Equal(t, "v2.0.0", merged.ClusterVersion)
			assert.Equal(t, "global-sa", merged.ServiceAccountName)
			assert.Equal(t, "30d", merged.RetentionPeriod)
		},
	})

	// multiple overrides
	f(opts{
		override: &vmv1beta1.VMClusterSpec{
			ClusterVersion:     "v2.0.0",
			ServiceAccountName: "global-sa",
		},
		validate: func(_, merged *vmv1beta1.VMClusterSpec, modified bool, err error) {
			assert.NoError(t, err)
			assert.True(t, modified)
			assert.Equal(t, "v2.0.0", merged.ClusterVersion)
			assert.Equal(t, "global-sa", merged.ServiceAccountName)
			assert.Equal(t, "30d", merged.RetentionPeriod)
		},
	}, opts{
		override: &vmv1beta1.VMClusterSpec{
			RetentionPeriod: "10d",
			ClusterVersion:  "v3.0.0",
		},
		validate: func(_, merged *vmv1beta1.VMClusterSpec, modified bool, err error) {
			assert.NoError(t, err)
			assert.True(t, modified)
			assert.Equal(t, "v3.0.0", merged.ClusterVersion)        // Cluster-specific override should take precedence
			assert.Equal(t, "global-sa", merged.ServiceAccountName) // From global override, unchanged by cluster override
			assert.Equal(t, "10d", merged.RetentionPeriod)          // From cluster override
		},
	})
}
