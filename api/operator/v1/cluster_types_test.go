package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func TestVTCluster_AvailableStorageNodeIDs(t *testing.T) {
	f := func(cr *VTCluster, requestsType string, want []int32) {
		t.Helper()
		assert.Equal(t, want, cr.AvailableStorageNodeIDs(requestsType))
	}

	cr := &VTCluster{
		Spec: VTClusterSpec{
			Storage: &VTStorage{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To(int32(5)),
				},
				MaintenanceSelectNodeIDs: []int32{1, 3},
				MaintenanceInsertNodeIDs: []int32{0, 4},
			},
		},
	}

	// select excludes maintenance nodes
	f(cr, "select", []int32{0, 2, 4})

	// insert excludes maintenance nodes
	f(cr, "insert", []int32{1, 2, 3})

	// no maintenance nodes
	f(&VTCluster{
		Spec: VTClusterSpec{
			Storage: &VTStorage{
				CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(3))},
			},
		},
	}, "select", []int32{0, 1, 2})
}

func TestVLCluster_AvailableStorageNodeIDs(t *testing.T) {
	f := func(cr *VLCluster, requestsType string, want []int32) {
		t.Helper()
		assert.Equal(t, want, cr.AvailableStorageNodeIDs(requestsType))
	}

	cr := &VLCluster{
		Spec: VLClusterSpec{
			VLStorage: &VLStorage{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To(int32(5)),
				},
				MaintenanceSelectNodeIDs: []int32{1, 3},
				MaintenanceInsertNodeIDs: []int32{0, 4},
			},
		},
	}

	// select excludes maintenance nodes
	f(cr, "select", []int32{0, 2, 4})

	// insert excludes maintenance nodes
	f(cr, "insert", []int32{1, 2, 3})

	// no maintenance nodes
	f(&VLCluster{
		Spec: VLClusterSpec{
			VLStorage: &VLStorage{
				CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(3))},
			},
		},
	}, "select", []int32{0, 1, 2})
}
