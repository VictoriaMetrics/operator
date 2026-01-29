package vmdistributed

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func TestBuildVMClusters(t *testing.T) {
	type opts struct {
		cr       *vmv1alpha1.VMDistributed
		validate func(*vmv1alpha1.VMDistributed, []*vmv1beta1.VMCluster)
	}
	f := func(o opts) {
		t.Helper()
		got, err := buildVMClusters(o.cr)
		assert.NoError(t, err)
		assert.Len(t, got, len(o.cr.Spec.Zones))
		o.validate(o.cr, got)
	}

	// override cluster version
	f(opts{
		cr: &vmv1alpha1.VMDistributed{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "ns",
			},
			Spec: vmv1alpha1.VMDistributedSpec{
				ZoneCommon: vmv1alpha1.VMDistributedZoneCommon{
					VMCluster: vmv1alpha1.VMDistributedZoneCluster{
						Spec: vmv1beta1.VMClusterSpec{
							ClusterVersion: "v0.2.0",
						},
					},
				},
				Zones: []vmv1alpha1.VMDistributedZone{
					{
						VMCluster: vmv1alpha1.VMDistributedZoneCluster{
							Name: "ref",
						},
					},
					{
						VMCluster: vmv1alpha1.VMDistributedZoneCluster{
							Name: "inline",
							Spec: vmv1beta1.VMClusterSpec{
								ClusterVersion: "v0.1.0",
							},
						},
					},
				},
			},
		},
		validate: func(cr *vmv1alpha1.VMDistributed, vmclusters []*vmv1beta1.VMCluster) {
			assert.Equal(t, "ref", vmclusters[0].Name)
			assert.Equal(t, "inline", vmclusters[1].Name)
			assert.Equal(t, cr.Spec.Zones[1].VMCluster.Spec.ClusterVersion, vmclusters[1].Spec.ClusterVersion)
			assert.Equal(t, cr.Spec.ZoneCommon.VMCluster.Spec.ClusterVersion, vmclusters[0].Spec.ClusterVersion)
		},
	})

	// add component specs from common
	f(opts{
		cr: &vmv1alpha1.VMDistributed{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "ns",
			},
			Spec: vmv1alpha1.VMDistributedSpec{
				ZoneCommon: vmv1alpha1.VMDistributedZoneCommon{
					VMCluster: vmv1alpha1.VMDistributedZoneCluster{
						Spec: vmv1beta1.VMClusterSpec{
							ClusterVersion: "v0.2.0",
							VMStorage: &vmv1beta1.VMStorage{
								VMInsertPort: "9999",
							},
						},
					},
				},
				Zones: []vmv1alpha1.VMDistributedZone{
					{
						VMCluster: vmv1alpha1.VMDistributedZoneCluster{
							Name: "ref",
						},
					},
					{
						VMCluster: vmv1alpha1.VMDistributedZoneCluster{
							Name: "inline",
							Spec: vmv1beta1.VMClusterSpec{
								ClusterVersion: "v0.1.0",
							},
						},
					},
				},
			},
		},
		validate: func(cr *vmv1alpha1.VMDistributed, vmclusters []*vmv1beta1.VMCluster) {
			assert.NotNil(t, vmclusters[0].Spec.VMStorage)
			assert.Equal(t, cr.Spec.ZoneCommon.VMCluster.Spec.VMStorage.VMInsertPort, vmclusters[0].Spec.VMStorage.VMInsertPort)
		},
	})
}
