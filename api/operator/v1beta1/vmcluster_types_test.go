package v1beta1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
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

func TestVMCluster_StorageNodeIDsUnderMaintenance(t *testing.T) {
	f := func(cr *VMCluster, want sets.Set[int32]) {
		t.Helper()
		assert.Equal(t, want, cr.StorageNodeIDsUnderMaintenance())
	}

	// node 3 is excluded from both insert and select routing
	f(&VMCluster{
		Spec: VMClusterSpec{
			VMStorage: &VMStorage{
				MaintenanceSelectNodeIDs: []int32{1, 3},
				MaintenanceInsertNodeIDs: []int32{0, 3},
			},
		},
	}, sets.New[int32](3))

	// no overlap between the two lists
	f(&VMCluster{
		Spec: VMClusterSpec{
			VMStorage: &VMStorage{
				MaintenanceSelectNodeIDs: []int32{1},
				MaintenanceInsertNodeIDs: []int32{0},
			},
		},
	}, sets.New[int32]())

	// global discovery enabled, maintenance node ids don't affect routing
	f(&VMCluster{
		Spec: VMClusterSpec{
			Discovery: &VMClusterDiscovery{Enabled: true},
			VMInsert:  &VMInsert{},
			VMSelect:  &VMSelect{},
			VMStorage: &VMStorage{
				MaintenanceSelectNodeIDs: []int32{3},
				MaintenanceInsertNodeIDs: []int32{3},
			},
		},
	}, nil)

	// vmselect discovery enabled only
	f(&VMCluster{
		Spec: VMClusterSpec{
			VMInsert: &VMInsert{},
			VMSelect: &VMSelect{Discovery: &VMClusterDiscovery{Enabled: true}},
			VMStorage: &VMStorage{
				MaintenanceSelectNodeIDs: []int32{3},
				MaintenanceInsertNodeIDs: []int32{3},
			},
		},
	}, nil)

	// global discovery disabled by vminsert and vmselect overrides
	f(&VMCluster{
		Spec: VMClusterSpec{
			Discovery: &VMClusterDiscovery{Enabled: true},
			VMInsert:  &VMInsert{Discovery: &VMClusterDiscovery{Enabled: false}},
			VMSelect:  &VMSelect{Discovery: &VMClusterDiscovery{Enabled: false}},
			VMStorage: &VMStorage{
				MaintenanceSelectNodeIDs: []int32{3},
				MaintenanceInsertNodeIDs: []int32{3},
			},
		},
	}, sets.New[int32](3))

	// no VMStorage
	f(&VMCluster{}, nil)
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

	// vmselect: a non-headless default service is allowed, see https://github.com/VictoriaMetrics/operator/issues/2487#issuecomment-5807946714
	f(VMClusterSpec{
		VMSelect: &VMSelect{
			ServiceSpec: &AdditionalServiceSpec{
				UseAsDefault: true,
				Spec:         corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP},
			},
		},
	}, false)

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
	}, false)

	f(VMClusterSpec{
		VMSelect: &VMSelect{
			ServiceSpec: &AdditionalServiceSpec{
				UseAsDefault: true,
				Spec:         corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer},
			},
		},
	}, false)

	// vmstorage: the default service must stay headless.
	// type=ClusterIP without clusterIP keeps it headless and is allowed
	f(VMClusterSpec{
		VMStorage: &VMStorage{
			ServiceSpec: &AdditionalServiceSpec{
				UseAsDefault: true,
				Spec:         corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP},
			},
		},
	}, false)

	f(VMClusterSpec{
		VMStorage: &VMStorage{
			ServiceSpec: &AdditionalServiceSpec{
				UseAsDefault: true,
				Spec:         corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer},
			},
		},
	}, true)

	f(VMClusterSpec{
		VMStorage: &VMStorage{
			ServiceSpec: &AdditionalServiceSpec{
				UseAsDefault: true,
				Spec:         corev1.ServiceSpec{ClusterIP: "1.1.1.1"},
			},
		},
	}, true)

	f(VMClusterSpec{
		VMStorage: &VMStorage{
			ServiceSpec: &AdditionalServiceSpec{
				UseAsDefault: true,
				Spec:         corev1.ServiceSpec{ClusterIPs: []string{"1.1.1.1"}},
			},
		},
	}, true)

	// vmstorage: overrides that keep the default service headless are allowed
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

	// an additional service is not restricted
	f(VMClusterSpec{
		VMStorage: &VMStorage{
			ServiceSpec: &AdditionalServiceSpec{
				Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer},
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

func TestInsertPorts_ValidateNoListenerNameCollision(t *testing.T) {
	f := func(ip *InsertPorts, name string, wantErr bool) {
		t.Helper()
		err := ip.ValidateNoListenerNameCollision([]HTTPListener{{Name: name, Addr: ":8480"}})
		if wantErr {
			assert.ErrorContains(t, err, "collides")
			return
		}
		assert.NoError(t, err)
	}
	ip := &InsertPorts{GraphitePort: "2003", InfluxPort: "8189", OpenTSDBPort: "4242", OpenTSDBHTTPPort: "4243"}

	// nil insertPorts reserves nothing
	f(nil, "graphite-tcp", false)

	// every generated port name
	for _, name := range []string{"graphite-tcp", "graphite-udp", "influx-tcp", "influx-udp", "opentsdb-tcp", "opentsdb-udp", "opentsdb-http"} {
		f(ip, name, true)
	}

	// protocol not configured, and unrelated name
	f(&InsertPorts{GraphitePort: "2003"}, "influx-tcp", false)
	f(ip, "http", false)
}

func TestVMCluster_Validate_ListenerNameCollision(t *testing.T) {
	f := func(spec VMClusterSpec, wantErr string) {
		t.Helper()
		err := (&VMCluster{Spec: spec}).Validate()
		if wantErr == "" {
			assert.NoError(t, err)
			return
		}
		assert.ErrorContains(t, err, wantErr)
	}
	listeners := func(name string) StandardAppsParams {
		return StandardAppsParams{HTTPListeners: []HTTPListener{{Name: name, Addr: ":8480"}}}
	}

	// vmselect and vminsert listener named like the cluster native port
	f(VMClusterSpec{VMSelect: &VMSelect{ClusterNativePort: "8401", StandardAppsParams: listeners("clusternative")}}, "clusternative")
	f(VMClusterSpec{VMInsert: &VMInsert{ClusterNativePort: "8400", StandardAppsParams: listeners("clusternative")}}, "clusternative")

	// the name is free when the native port is not configured
	f(VMClusterSpec{VMSelect: &VMSelect{StandardAppsParams: listeners("clusternative")}}, "")

	// vmstorage listener named like generated vminsert and vmselect ports
	f(VMClusterSpec{VMStorage: &VMStorage{StandardAppsParams: listeners("vminsert")}}, "vminsert")
	f(VMClusterSpec{VMStorage: &VMStorage{StandardAppsParams: listeners("vmselect")}}, "vmselect")
	f(VMClusterSpec{VMStorage: &VMStorage{StandardAppsParams: listeners("http")}}, "")

	// vminsert listener named like a generated insertPorts name
	f(VMClusterSpec{VMInsert: &VMInsert{
		InsertPorts:        &InsertPorts{GraphitePort: "2003"},
		StandardAppsParams: listeners("graphite-tcp"),
	}}, "graphite-tcp")
}
