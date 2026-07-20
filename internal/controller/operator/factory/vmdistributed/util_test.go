package vmdistributed

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/utils/ptr"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func TestMergeSpecs(t *testing.T) {
	t.Run("VMCluster", func(t *testing.T) {
		f := func(common, zone *vmv1beta1.VMClusterSpec, zoneName string, expected *vmv1beta1.VMClusterSpec) {
			t.Helper()
			got, err := mergeSpecs(common, zone, zoneName)
			assert.NoError(t, err)
			assert.Equal(t, expected, got)
		}

		// zone-specific retention period
		f(&vmv1beta1.VMClusterSpec{
			RetentionPeriod: "1d",
			VMStorage: &vmv1beta1.VMStorage{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To(int32(1)),
				},
			},
		}, &vmv1beta1.VMClusterSpec{
			RetentionPeriod: "30d",
		}, "zone-a", &vmv1beta1.VMClusterSpec{
			RetentionPeriod: "30d",
			VMStorage: &vmv1beta1.VMStorage{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To(int32(1)),
				},
			},
		})

		// %ZONE% templating
		f(&vmv1beta1.VMClusterSpec{
			RetentionPeriod: "1d",
			VMStorage: &vmv1beta1.VMStorage{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
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
				CommonAppsParams: vmv1beta1.CommonAppsParams{
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
			assert.NoError(t, err)
			assert.Equal(t, expected, got)
		}

		f(&vmv1alpha1.VMDistributedZoneAgentSpec{
			CommonAppsParams: vmv1beta1.CommonAppsParams{
				NodeSelector: map[string]string{
					"topology.kubernetes.io/zone": "%ZONE%",
				},
			},
		}, &vmv1alpha1.VMDistributedZoneAgentSpec{}, "zone-b", &vmv1alpha1.VMDistributedZoneAgentSpec{
			CommonAppsParams: vmv1beta1.CommonAppsParams{
				NodeSelector: map[string]string{
					"topology.kubernetes.io/zone": "zone-b",
				},
			},
		})
	})
}
