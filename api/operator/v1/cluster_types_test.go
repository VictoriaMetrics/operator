package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

//nolint:dupl
func TestVTCluster_AvailableStorageNodeIDs(t *testing.T) {
	f := func(cr *VTCluster, kind vmv1beta1.ClusterComponent, want []int32) {
		t.Helper()
		assert.Equal(t, want, cr.AvailableStorageNodeIDs(kind))
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
	f(cr, vmv1beta1.ClusterComponentSelect, []int32{0, 2, 4})

	// insert excludes maintenance nodes
	f(cr, vmv1beta1.ClusterComponentInsert, []int32{1, 2, 3})

	// no maintenance nodes
	f(&VTCluster{
		Spec: VTClusterSpec{
			Storage: &VTStorage{
				CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(3))},
			},
		},
	}, vmv1beta1.ClusterComponentSelect, []int32{0, 1, 2})
}

//nolint:dupl
func TestVLCluster_AvailableStorageNodeIDs(t *testing.T) {
	f := func(cr *VLCluster, kind vmv1beta1.ClusterComponent, want []int32) {
		t.Helper()
		assert.Equal(t, want, cr.AvailableStorageNodeIDs(kind))
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
	f(cr, vmv1beta1.ClusterComponentSelect, []int32{0, 2, 4})

	// insert excludes maintenance nodes
	f(cr, vmv1beta1.ClusterComponentInsert, []int32{1, 2, 3})

	// no maintenance nodes
	f(&VLCluster{
		Spec: VLClusterSpec{
			VLStorage: &VLStorage{
				CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(3))},
			},
		},
	}, vmv1beta1.ClusterComponentSelect, []int32{0, 1, 2})
}

func TestVLSingle_PrefixedName(t *testing.T) {
	cr := &VLSingle{}
	cr.Name = "myapp"
	assert.Equal(t, "vlsingle-myapp", cr.PrefixedName())
}

func TestVTSingle_PrefixedName(t *testing.T) {
	cr := &VTSingle{}
	cr.Name = "myapp"
	assert.Equal(t, "vtsingle-myapp", cr.PrefixedName())
}

func TestVLAgent_PrefixedName(t *testing.T) {
	cr := &VLAgent{}
	cr.Name = "myapp"
	assert.Equal(t, "vlagent-myapp", cr.PrefixedName())
}

func TestVMAnomaly_PrefixedName(t *testing.T) {
	cr := &VMAnomaly{}
	cr.Name = "myapp"
	assert.Equal(t, "vmanomaly-myapp", cr.PrefixedName())
}

func TestVLCluster_PrefixedName(t *testing.T) {
	f := func(name string, kind vmv1beta1.ClusterComponent, want string) {
		t.Helper()
		cr := &VLCluster{}
		cr.Name = name
		assert.Equal(t, want, cr.PrefixedName(kind))
	}

	f("myapp", vmv1beta1.ClusterComponentSelect, "vlselect-myapp")
	f("myapp", vmv1beta1.ClusterComponentInsert, "vlinsert-myapp")
	f("myapp", vmv1beta1.ClusterComponentStorage, "vlstorage-myapp")
}

func TestVTCluster_PrefixedName(t *testing.T) {
	f := func(name string, kind vmv1beta1.ClusterComponent, want string) {
		t.Helper()
		cr := &VTCluster{}
		cr.Name = name
		assert.Equal(t, want, cr.PrefixedName(kind))
	}

	f("myapp", vmv1beta1.ClusterComponentSelect, "vtselect-myapp")
	f("myapp", vmv1beta1.ClusterComponentInsert, "vtinsert-myapp")
	f("myapp", vmv1beta1.ClusterComponentStorage, "vtstorage-myapp")
}
