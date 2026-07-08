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
	f := func(name string, omit bool, want string) {
		t.Helper()
		cr := &VLSingle{Spec: VLSingleSpec{UseLegacyNaming: omit}}
		cr.Name = name
		assert.Equal(t, want, cr.PrefixedName())
	}

	f("myapp", false, "vlsingle-myapp")
	f("myapp", true, "myapp")
}

func TestVTSingle_PrefixedName(t *testing.T) {
	f := func(name string, omit bool, want string) {
		t.Helper()
		cr := &VTSingle{Spec: VTSingleSpec{UseLegacyNaming: omit}}
		cr.Name = name
		assert.Equal(t, want, cr.PrefixedName())
	}

	f("myapp", false, "vtsingle-myapp")
	f("myapp", true, "myapp")
}

func TestVLAgent_PrefixedName(t *testing.T) {
	f := func(name string, omit bool, want string) {
		t.Helper()
		cr := &VLAgent{Spec: VLAgentSpec{UseLegacyNaming: omit}}
		cr.Name = name
		assert.Equal(t, want, cr.PrefixedName())
	}

	f("myapp", false, "vlagent-myapp")
	f("myapp", true, "myapp")
}

func TestVMAnomaly_PrefixedName(t *testing.T) {
	f := func(name string, omit bool, want string) {
		t.Helper()
		cr := &VMAnomaly{Spec: VMAnomalySpec{UseLegacyNaming: omit}}
		cr.Name = name
		assert.Equal(t, want, cr.PrefixedName())
	}

	f("myapp", false, "vmanomaly-myapp")
	f("myapp", true, "myapp")
}

func TestVLCluster_PrefixedName(t *testing.T) {
	f := func(name string, omit bool, kind vmv1beta1.ClusterComponent, want string) {
		t.Helper()
		cr := &VLCluster{Spec: VLClusterSpec{UseLegacyNaming: omit}}
		cr.Name = name
		assert.Equal(t, want, cr.PrefixedName(kind))
	}

	// default — <component>-<name>
	f("myapp", false, vmv1beta1.ClusterComponentSelect, "vlselect-myapp")
	f("myapp", false, vmv1beta1.ClusterComponentInsert, "vlinsert-myapp")
	f("myapp", false, vmv1beta1.ClusterComponentStorage, "vlstorage-myapp")

	// useLegacyNaming — <name>-<component>
	f("myapp", true, vmv1beta1.ClusterComponentSelect, "myapp-vlselect")
	f("myapp", true, vmv1beta1.ClusterComponentInsert, "myapp-vlinsert")
	f("myapp", true, vmv1beta1.ClusterComponentStorage, "myapp-vlstorage")
}

func TestVTCluster_PrefixedName(t *testing.T) {
	f := func(name string, omit bool, kind vmv1beta1.ClusterComponent, want string) {
		t.Helper()
		cr := &VTCluster{Spec: VTClusterSpec{UseLegacyNaming: omit}}
		cr.Name = name
		assert.Equal(t, want, cr.PrefixedName(kind))
	}

	// default — <component>-<name>
	f("myapp", false, vmv1beta1.ClusterComponentSelect, "vtselect-myapp")
	f("myapp", false, vmv1beta1.ClusterComponentInsert, "vtinsert-myapp")
	f("myapp", false, vmv1beta1.ClusterComponentStorage, "vtstorage-myapp")

	// useLegacyNaming — <name>-<component>
	f("myapp", true, vmv1beta1.ClusterComponentSelect, "myapp-vtselect")
	f("myapp", true, vmv1beta1.ClusterComponentInsert, "myapp-vtinsert")
	f("myapp", true, vmv1beta1.ClusterComponentStorage, "myapp-vtstorage")
}

//nolint:dupl
func TestVLCluster_GetStorageVolumeName(t *testing.T) {
	f := func(cr *VLCluster, want string) {
		t.Helper()
		assert.Equal(t, want, cr.GetStorageVolumeName())
	}

	// nil cluster — default name, no panic
	f(nil, "vlstorage-db")

	// no VLStorage — default name
	f(&VLCluster{}, "vlstorage-db")

	// VLStorage without storage spec — default name
	f(&VLCluster{Spec: VLClusterSpec{VLStorage: &VLStorage{}}}, "vlstorage-db")

	// VLStorage with storage spec but empty volume name — default name
	f(&VLCluster{Spec: VLClusterSpec{VLStorage: &VLStorage{Storage: &vmv1beta1.StorageSpec{}}}}, "vlstorage-db")

	// useLegacyNaming without explicit volume name — legacy name
	f(&VLCluster{Spec: VLClusterSpec{UseLegacyNaming: true, VLStorage: &VLStorage{}}}, "vlstorage-volume")

	// explicit volume claim template name wins regardless of useLegacyNaming
	f(&VLCluster{
		Spec: VLClusterSpec{
			UseLegacyNaming: true,
			VLStorage: &VLStorage{
				Storage: &vmv1beta1.StorageSpec{
					VolumeClaimTemplate: vmv1beta1.EmbeddedPersistentVolumeClaim{
						EmbeddedObjectMetadata: vmv1beta1.EmbeddedObjectMetadata{Name: "custom-storage"},
					},
				},
			},
		},
	}, "custom-storage")
}

//nolint:dupl
func TestVTCluster_GetStorageVolumeName(t *testing.T) {
	f := func(cr *VTCluster, want string) {
		t.Helper()
		assert.Equal(t, want, cr.GetStorageVolumeName())
	}

	// nil cluster — default name, no panic
	f(nil, "vtstorage-db")

	// no Storage — default name
	f(&VTCluster{}, "vtstorage-db")

	// Storage without storage spec — default name
	f(&VTCluster{Spec: VTClusterSpec{Storage: &VTStorage{}}}, "vtstorage-db")

	// Storage with storage spec but empty volume name — default name
	f(&VTCluster{Spec: VTClusterSpec{Storage: &VTStorage{Storage: &vmv1beta1.StorageSpec{}}}}, "vtstorage-db")

	// useLegacyNaming without explicit volume name — legacy name
	f(&VTCluster{Spec: VTClusterSpec{UseLegacyNaming: true, Storage: &VTStorage{}}}, "vtstorage-volume")

	// explicit volume claim template name wins regardless of useLegacyNaming
	f(&VTCluster{
		Spec: VTClusterSpec{
			UseLegacyNaming: true,
			Storage: &VTStorage{
				Storage: &vmv1beta1.StorageSpec{
					VolumeClaimTemplate: vmv1beta1.EmbeddedPersistentVolumeClaim{
						EmbeddedObjectMetadata: vmv1beta1.EmbeddedObjectMetadata{Name: "custom-storage"},
					},
				},
			},
		},
	}, "custom-storage")
}
