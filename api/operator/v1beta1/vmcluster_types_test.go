package v1beta1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestVMCluster_Backup(t *testing.T) {
	// nil VMStorage
	cr := VMCluster{}
	assert.Nil(t, cr.Backup())
	assert.Equal(t, "", cr.SnapshotCreatePath("localhost"))
	assert.Equal(t, "", cr.SnapshotDeletePath("localhost"))

	// with VMStorage and VMBackup configured
	vmBackup := &VMBackup{}
	cr = VMCluster{
		Spec: VMClusterSpec{
			VMStorage: &VMStorage{
				StandardAppsParams: StandardAppsParams{
					CommonAppsParams: CommonAppsParams{Port: "8482"},
				},
				VMBackup: vmBackup,
			},
		},
	}
	assert.Same(t, vmBackup, cr.Backup())
	assert.Equal(t, "http://localhost:8482/snapshot/create", cr.SnapshotCreatePath("localhost"))
	assert.Equal(t, "http://localhost:8482/snapshot/delete", cr.SnapshotDeletePath("localhost"))
}

func TestVMCluster_AvailableStorageNodeIDs(t *testing.T) {
	f := func(cr *VMCluster, kind ClusterComponent, want []int32) {
		t.Helper()
		assert.Equal(t, want, cr.AvailableStorageNodeIDs(kind))
	}

	cr := &VMCluster{
		Spec: VMClusterSpec{
			VMStorage: &VMStorage{
				StandardAppsParams: StandardAppsParams{
					CommonAppsParams: CommonAppsParams{
						ReplicaCount: ptr.To(int32(5)),
					},
				},
				MaintenanceSelectNodeIDs: []int32{1, 3},
				MaintenanceInsertNodeIDs: []int32{0, 4},
			},
		},
	}

	// select excludes maintenance nodes
	f(cr, ClusterComponentSelect, []int32{0, 2, 4})

	// insert excludes maintenance nodes
	f(cr, ClusterComponentInsert, []int32{1, 2, 3})

	// no maintenance nodes
	f(&VMCluster{
		Spec: VMClusterSpec{
			VMStorage: &VMStorage{
				StandardAppsParams: StandardAppsParams{
					CommonAppsParams: CommonAppsParams{
						ReplicaCount: ptr.To(int32(3)),
					},
				},
			},
		},
	}, ClusterComponentSelect, []int32{0, 1, 2})
}

func TestVMCluster_Validate(t *testing.T) {
	f := func(spec VMClusterSpec, wantErr bool) {
		t.Helper()
		cr := &VMCluster{Spec: spec}
		if wantErr {
			assert.Error(t, cr.Validate())
		} else {
			assert.NoError(t, cr.Validate())
		}
	}

	// empty spec
	f(VMClusterSpec{}, false)

	// downsampling without license
	f(VMClusterSpec{
		Downsampling: &DownsamplingConfig{
			Rules: []DownsamplingRule{{
				Periods: []DownsamplingPeriod{
					{Offset: "30d", Interval: "10m"},
				},
			}},
		},
	}, true)

	// downsampling with valid config
	f(VMClusterSpec{
		License: testLicense,
		Downsampling: &DownsamplingConfig{
			Rules: []DownsamplingRule{{
				Periods: []DownsamplingPeriod{
					{Offset: "30d", Interval: "10m"},
				},
			}},
		},
	}, false)

	// downsampling with filter and dedupInterval
	f(VMClusterSpec{
		License: testLicense,
		Downsampling: &DownsamplingConfig{
			Rules: []DownsamplingRule{{
				Filter: `{env="prod"}`,
				Periods: []DownsamplingPeriod{
					{Offset: "90d", Interval: "1h"},
				},
			}},
			DedupInterval: "1m",
		},
	}, false)

	// downsampling - multiple periods per rule
	f(VMClusterSpec{
		License: testLicense,
		Downsampling: &DownsamplingConfig{
			Rules: []DownsamplingRule{{
				Periods: []DownsamplingPeriod{
					{Offset: "30d", Interval: "10m"},
					{Offset: "180d", Interval: "1h"},
				},
			}},
		},
	}, false)

	// downsampling - duplicate filter
	f(VMClusterSpec{
		License: testLicense,
		Downsampling: &DownsamplingConfig{
			Rules: []DownsamplingRule{
				{Periods: []DownsamplingPeriod{{Offset: "30d", Interval: "10m"}}},
				{Periods: []DownsamplingPeriod{{Offset: "180d", Interval: "1h"}}},
			},
		},
	}, true)

	// downsampling - offset not a multiple of interval
	f(VMClusterSpec{
		License: testLicense,
		Downsampling: &DownsamplingConfig{
			Rules: []DownsamplingRule{
				{Periods: []DownsamplingPeriod{{Offset: "1d", Interval: "7m"}}},
			},
		},
	}, true)

	// downsampling - period interval not a multiple of dedupInterval
	f(VMClusterSpec{
		License: testLicense,
		Downsampling: &DownsamplingConfig{
			Rules: []DownsamplingRule{
				{Periods: []DownsamplingPeriod{{Offset: "30d", Interval: "10m"}}},
			},
			DedupInterval: "7m",
		},
	}, true)

	// downsampling - period interval is a multiple of dedupInterval
	f(VMClusterSpec{
		License: testLicense,
		Downsampling: &DownsamplingConfig{
			Rules: []DownsamplingRule{
				{Periods: []DownsamplingPeriod{{Offset: "30d", Interval: "10m"}}},
			},
			DedupInterval: "5m",
		},
	}, false)

	// retention filters without vmstorage section — no error (vmstorage is nil)
	f(VMClusterSpec{
		License: testLicense,
	}, false)

	// retention filters without license
	f(VMClusterSpec{
		VMStorage: &VMStorage{
			RetentionFilters: &RetentionFiltersConfig{{Filter: `{env="dev"}`, Retention: "3d"}},
		},
	}, true)

	// retention filters with valid config
	f(VMClusterSpec{
		License:         testLicense,
		RetentionPeriod: "30",
		VMStorage: &VMStorage{
			RetentionFilters: &RetentionFiltersConfig{{Filter: `{env="dev"}`, Retention: "3d"}},
		},
	}, false)

	// retention filters - invalid filter
	f(VMClusterSpec{
		License: testLicense,
		VMStorage: &VMStorage{
			RetentionFilters: &RetentionFiltersConfig{{Filter: "not-a-filter", Retention: "3d"}},
		},
	}, true)

	// retention filters - invalid retention
	f(VMClusterSpec{
		License: testLicense,
		VMStorage: &VMStorage{
			RetentionFilters: &RetentionFiltersConfig{{Filter: `{env="dev"}`, Retention: "bad"}},
		},
	}, true)

	// retention filters - retention exceeds retentionPeriod
	f(VMClusterSpec{
		License:         testLicense,
		RetentionPeriod: "30d",
		VMStorage: &VMStorage{
			RetentionFilters: &RetentionFiltersConfig{{Filter: `{env="dev"}`, Retention: "1y"}},
		},
	}, true)

	// retention filters - retention equal to retentionPeriod is ok
	f(VMClusterSpec{
		License:         testLicense,
		RetentionPeriod: "1y",
		VMStorage: &VMStorage{
			RetentionFilters: &RetentionFiltersConfig{{Filter: `{env="dev"}`, Retention: "1y"}},
		},
	}, false)

	// discovery without license
	f(VMClusterSpec{
		VMInsert:  &VMInsert{},
		Discovery: &VMClusterDiscovery{Enabled: true},
	}, true)

	// discovery with license
	f(VMClusterSpec{
		VMInsert:  &VMInsert{},
		License:   testLicense,
		Discovery: &VMClusterDiscovery{Enabled: true},
	}, false)

	// discovery with invalid filter regexp
	f(VMClusterSpec{
		VMInsert:  &VMInsert{},
		License:   testLicense,
		Discovery: &VMClusterDiscovery{Enabled: true, Filter: "[invalid"},
	}, true)

	// discovery with valid filter regexp
	f(VMClusterSpec{
		VMInsert:  &VMInsert{},
		License:   testLicense,
		Discovery: &VMClusterDiscovery{Enabled: true, Filter: `vmstorage-test-[0-3]\.`},
	}, false)

	// global discovery + maintenanceInsertNodeIDs
	f(VMClusterSpec{
		License:   testLicense,
		Discovery: &VMClusterDiscovery{Enabled: true},
		VMInsert:  &VMInsert{},
		VMStorage: &VMStorage{MaintenanceInsertNodeIDs: []int32{0}},
	}, true)

	// global discovery + maintenanceSelectNodeIDs
	f(VMClusterSpec{
		License:   testLicense,
		Discovery: &VMClusterDiscovery{Enabled: true},
		VMSelect:  &VMSelect{},
		VMStorage: &VMStorage{MaintenanceSelectNodeIDs: []int32{1}},
	}, true)

	// component override disables vmselect discovery: maintenanceSelectNodeIDs is allowed
	f(VMClusterSpec{
		License:   testLicense,
		Discovery: &VMClusterDiscovery{Enabled: true},
		VMInsert:  &VMInsert{},
		VMSelect:  &VMSelect{Discovery: &VMClusterDiscovery{Enabled: false}},
		VMStorage: &VMStorage{MaintenanceSelectNodeIDs: []int32{1}},
	}, false)

	// component override disables vminsert discovery: maintenanceInsertNodeIDs is allowed
	f(VMClusterSpec{
		License:   testLicense,
		Discovery: &VMClusterDiscovery{Enabled: true},
		VMInsert:  &VMInsert{Discovery: &VMClusterDiscovery{Enabled: false}},
		VMStorage: &VMStorage{MaintenanceInsertNodeIDs: []int32{0}},
	}, false)

	// component-level discovery enabled without global, requires license
	f(VMClusterSpec{
		VMInsert: &VMInsert{Discovery: &VMClusterDiscovery{Enabled: true}},
	}, true)

	// component-level discovery enabled with license
	f(VMClusterSpec{
		License:  testLicense,
		VMInsert: &VMInsert{Discovery: &VMClusterDiscovery{Enabled: true}},
	}, false)

	// extraStorageNodes with unique addresses
	f(VMClusterSpec{
		VMSelect: &VMSelect{
			ExtraStorageNodes: []VMStorageNode{
				{Addr: "localhost:10101"},
				{Addr: "localhost:10102"},
			},
		},
	}, false)

	// extraStorageNodes with duplicate addresses
	f(VMClusterSpec{
		VMSelect: &VMSelect{
			ExtraStorageNodes: []VMStorageNode{
				{Addr: "localhost:10101"},
				{Addr: "localhost:10101"},
			},
		},
	}, true)

	// extraStorageNodes duplicating extraArgs storageNode
	f(VMClusterSpec{
		VMSelect: &VMSelect{
			StandardAppsParams: StandardAppsParams{
				CommonAppsParams: CommonAppsParams{
					ExtraArgs: map[string]string{"storageNode": "localhost:10101"},
				},
			},
			ExtraStorageNodes: []VMStorageNode{
				{Addr: "localhost:10101"},
			},
		},
	}, true)

	// extraStorageNodes with an empty addr
	f(VMClusterSpec{
		VMSelect: &VMSelect{
			ExtraStorageNodes: []VMStorageNode{
				{Addr: ""},
			},
		},
	}, true)

	// vmselect: useAsDefault with an explicit service type must be rejected (default service is headless)
	f(VMClusterSpec{
		VMSelect: &VMSelect{
			ServiceSpec: &AdditionalServiceSpec{
				UseAsDefault: true,
				Spec: corev1.ServiceSpec{
					Type:      corev1.ServiceTypeClusterIP,
					ClusterIP: "1.1.1.1",
				},
			},
		},
	}, true)

	f(VMClusterSpec{
		VMSelect: &VMSelect{
			ServiceSpec: &AdditionalServiceSpec{
				UseAsDefault: true,
				Spec: corev1.ServiceSpec{
					Type:      corev1.ServiceTypeClusterIP,
					ClusterIP: corev1.ClusterIPNone,
				},
			},
		},
	}, false)

	// vmselect: useAsDefault without an explicit type is allowed
	f(VMClusterSpec{
		VMSelect: &VMSelect{
			ServiceSpec: &AdditionalServiceSpec{
				UseAsDefault: true,
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{{Name: "http", Port: 8481}},
				},
			},
		},
	}, false)

	// vmselect: explicit type without useAsDefault creates a separate service - allowed
	f(VMClusterSpec{
		VMSelect: &VMSelect{
			ServiceSpec: &AdditionalServiceSpec{
				Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer},
			},
		},
	}, false)

	// vmstorage: useAsDefault with cluster IP set must be rejected (default service is headless)
	f(VMClusterSpec{
		VMStorage: &VMStorage{
			ServiceSpec: &AdditionalServiceSpec{
				UseAsDefault: true,
				Spec: corev1.ServiceSpec{
					Type:      corev1.ServiceTypeClusterIP,
					ClusterIP: "1.1.1.1",
				},
			},
		},
	}, true)

	// vmstorage: useAsDefault without cluster IP set must be allowed
	f(VMClusterSpec{
		VMStorage: &VMStorage{
			ServiceSpec: &AdditionalServiceSpec{
				UseAsDefault: true,
				Spec: corev1.ServiceSpec{
					Type:      corev1.ServiceTypeClusterIP,
					ClusterIP: corev1.ClusterIPNone,
				},
			},
		},
	}, false)

	// vmstorage: useAsDefault without an explicit type is allowed
	f(VMClusterSpec{
		VMStorage: &VMStorage{
			ServiceSpec: &AdditionalServiceSpec{
				UseAsDefault: true,
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{{Name: "http", Port: 8482}},
				},
			},
		},
	}, false)

	// vminsert: useAsDefault with an explicit type is allowed (default service is not headless)
	f(VMClusterSpec{
		VMInsert: &VMInsert{
			ServiceSpec: &AdditionalServiceSpec{
				UseAsDefault: true,
				Spec:         corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer},
			},
		},
	}, false)

	// extraStorageNodes colliding with a managed vmstorage pod address
	{
		cr := &VMCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test"},
			Spec: VMClusterSpec{
				VMStorage: &VMStorage{
					StandardAppsParams: StandardAppsParams{
						CommonAppsParams: CommonAppsParams{ReplicaCount: ptr.To(int32(1))},
					},
					VMSelectPort: "8481",
				},
				VMSelect: &VMSelect{},
			},
		}
		managedAddr := PodDNSAddress(cr.PrefixedName(ClusterComponentStorage), 0, cr.Namespace, cr.Spec.VMStorage.VMSelectPort, cr.Spec.ClusterDomainName)
		cr.Spec.VMSelect.ExtraStorageNodes = []VMStorageNode{{Addr: managedAddr}}
		assert.Error(t, cr.Validate())
	}
}

func TestVMCluster_PrefixedName(t *testing.T) {
	f := func(name string, omit bool, kind ClusterComponent, want string) {
		t.Helper()
		cr := &VMCluster{Spec: VMClusterSpec{UseLegacyNaming: omit}}
		cr.Name = name
		assert.Equal(t, want, cr.PrefixedName(kind))
	}

	// default — <component>-<name>
	f("myapp", false, ClusterComponentSelect, "vmselect-myapp")
	f("myapp", false, ClusterComponentInsert, "vminsert-myapp")
	f("myapp", false, ClusterComponentStorage, "vmstorage-myapp")

	// useLegacyNaming — <name>-<component>
	f("myapp", true, ClusterComponentSelect, "myapp-vmselect")
	f("myapp", true, ClusterComponentInsert, "myapp-vminsert")
	f("myapp", true, ClusterComponentStorage, "myapp-vmstorage")
}
