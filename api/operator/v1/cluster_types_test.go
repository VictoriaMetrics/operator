package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/util/sets"
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

func TestVLCluster_RequestsLoadBalancerHTTPListenersValidate(t *testing.T) {
	for _, tc := range []struct {
		name    string
		enabled bool
		params  vmv1beta1.CommonAppsParams
		wantErr string
	}{
		{
			name:    "reserved listener name",
			enabled: true,
			params:  vmv1beta1.CommonAppsParams{HTTPListeners: []vmv1beta1.HTTPListener{{Name: "http", Port: 8429}}},
			wantErr: "requestsLoadBalancer: httpListeners name=\"http\" is reserved",
		},
		{
			name:    "legacy listener list",
			enabled: true,
			params: vmv1beta1.CommonAppsParams{
				ExtraArgs:     map[string]string{"httpListenAddr": ":8427,:8429"},
				HTTPListeners: []vmv1beta1.HTTPListener{{Name: "plain", Port: 8430}},
			},
			wantErr: "requestsLoadBalancer: httpListeners cannot be combined with listeners defined in extraArgs.httpListenAddr",
		},
		{
			name:    "valid listeners",
			enabled: true,
			params:  vmv1beta1.CommonAppsParams{HTTPListeners: []vmv1beta1.HTTPListener{{Name: "plain", Port: 8429}}},
		},
		{
			name:   "disabled load balancer",
			params: vmv1beta1.CommonAppsParams{HTTPListeners: []vmv1beta1.HTTPListener{{Name: "http", Port: 8429}}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cr := &VLCluster{Spec: VLClusterSpec{RequestsLoadBalancer: vmv1beta1.VMAuthLoadBalancer{
				Enabled: tc.enabled,
				Spec:    vmv1beta1.VMAuthLoadBalancerSpec{CommonAppsParams: tc.params},
			}}}
			err := cr.Validate()
			if tc.wantErr == "" {
				assert.NoError(t, err)
			} else {
				assert.ErrorContains(t, err, tc.wantErr)
			}
		})
	}
}

func TestVTCluster_RequestsLoadBalancerHTTPListenersValidate(t *testing.T) {
	for _, tc := range []struct {
		name    string
		enabled bool
		wantErr string
	}{
		{name: "enabled", enabled: true, wantErr: "requestsLoadBalancer: httpListeners name=\"http\" is reserved"},
		{name: "disabled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cr := &VTCluster{Spec: VTClusterSpec{RequestsLoadBalancer: vmv1beta1.VMAuthLoadBalancer{
				Enabled: tc.enabled,
				Spec: vmv1beta1.VMAuthLoadBalancerSpec{CommonAppsParams: vmv1beta1.CommonAppsParams{
					HTTPListeners: []vmv1beta1.HTTPListener{{Name: "http", Port: 8429}},
				}},
			}}}
			err := cr.Validate()
			if tc.wantErr == "" {
				assert.NoError(t, err)
			} else {
				assert.ErrorContains(t, err, tc.wantErr)
			}
		})
	}
}

func TestVTCluster_StorageNodeIDsUnderMaintenance(t *testing.T) {
	f := func(cr *VTCluster, want sets.Set[int32]) {
		t.Helper()
		assert.Equal(t, want, cr.StorageNodeIDsUnderMaintenance())
	}

	f(&VTCluster{
		Spec: VTClusterSpec{
			Storage: &VTStorage{
				MaintenanceSelectNodeIDs: []int32{1, 3},
				MaintenanceInsertNodeIDs: []int32{0, 3},
			},
		},
	}, sets.New[int32](3))

	f(&VTCluster{}, nil)
}

func TestVLCluster_StorageNodeIDsUnderMaintenance(t *testing.T) {
	f := func(cr *VLCluster, want sets.Set[int32]) {
		t.Helper()
		assert.Equal(t, want, cr.StorageNodeIDsUnderMaintenance())
	}

	f(&VLCluster{
		Spec: VLClusterSpec{
			VLStorage: &VLStorage{
				MaintenanceSelectNodeIDs: []int32{1, 3},
				MaintenanceInsertNodeIDs: []int32{0, 3},
			},
		},
	}, sets.New[int32](3))

	f(&VLCluster{}, nil)
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
