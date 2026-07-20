package v1beta1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/utils/ptr"
)

func TestVMBackup_SnapshotDeletePathWithFlags(t *testing.T) {
	type opts struct {
		host      string
		port      string
		extraArgs map[string]string
		want      string
	}
	f := func(o opts) {
		t.Helper()
		cr := VMBackup{}
		got := cr.SnapshotDeletePathWithFlags(o.host, o.port, o.extraArgs)
		assert.Equal(t, o.want, got)
	}

	// default delete path
	f(opts{
		host: "localhost",
		port: "8428",
		want: "http://localhost:8428/snapshot/delete",
	})

	// delete path with prefix
	f(opts{
		host:      "127.0.0.1",
		port:      "8428",
		extraArgs: map[string]string{httpPathPrefixFlag: "/pref-1", "other-flag": "other-value"},
		want:      "http://127.0.0.1:8428/pref-1/snapshot/delete",
	})

	// delete path with auth key
	f(opts{
		host:      "127.0.0.1",
		port:      "8428",
		extraArgs: map[string]string{httpPathPrefixFlag: "/pref-1", "other-flag": "other-value", snapshotAuthKeyFlag: "test"},
		want:      "http://127.0.0.1:8428/pref-1/snapshot/delete?authKey=test",
	})
}

func TestVMBackup_SnapshotCreatePathWithFlags(t *testing.T) {
	type opts struct {
		host      string
		port      string
		extraArgs map[string]string
		want      string
	}
	f := func(o opts) {
		t.Helper()
		cr := VMBackup{}
		got := cr.SnapshotCreatePathWithFlags(o.host, o.port, o.extraArgs)
		assert.Equal(t, o.want, got)
	}

	// base ok
	f(opts{
		host: "localhost",
		port: "8429",
		want: "http://localhost:8429/snapshot/create",
	})

	// with prefix
	f(opts{
		host: "127.0.0.1",
		port: "8429",
		extraArgs: map[string]string{
			"http.pathPrefix": "/prefix/custom",
		},
		want: "http://127.0.0.1:8429/prefix/custom/snapshot/create",
	})

	// with prefix and auth key
	f(opts{
		host: "localhost",
		port: "8429",
		extraArgs: map[string]string{
			"http.pathPrefix": "/prefix/custom",
			"snapshotAuthKey": "some-auth-key",
		},
		want: "http://localhost:8429/prefix/custom/snapshot/create?authKey=some-auth-key",
	})
}

func TestVMCluster_AvailableStorageNodeIDs(t *testing.T) {
	f := func(cr *VMCluster, kind ClusterComponent, want []int32) {
		t.Helper()
		assert.Equal(t, want, cr.AvailableStorageNodeIDs(kind))
	}

	cr := &VMCluster{
		Spec: VMClusterSpec{
			VMStorage: &VMStorage{
				CommonAppsParams: CommonAppsParams{
					ReplicaCount: ptr.To(int32(5)),
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
				CommonAppsParams: CommonAppsParams{ReplicaCount: ptr.To(int32(3))},
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
			Rules: []DownsamplingRule{{Periods: []DownsamplingPeriod{{Offset: "30d", Interval: "10m"}}}},
		},
	}, true)

	// downsampling with valid config
	f(VMClusterSpec{
		License: testLicense,
		Downsampling: &DownsamplingConfig{
			Rules: []DownsamplingRule{{Periods: []DownsamplingPeriod{{Offset: "30d", Interval: "10m"}}}},
		},
	}, false)

	// downsampling with filter and dedupInterval
	f(VMClusterSpec{
		License: testLicense,
		Downsampling: &DownsamplingConfig{
			Rules:         []DownsamplingRule{{Filter: `{env="prod"}`, Periods: []DownsamplingPeriod{{Offset: "90d", Interval: "1h"}}}},
			DedupInterval: "1m",
		},
	}, false)

	// downsampling - multiple periods per rule
	f(VMClusterSpec{
		License: testLicense,
		Downsampling: &DownsamplingConfig{
			Rules: []DownsamplingRule{{Periods: []DownsamplingPeriod{
				{Offset: "30d", Interval: "10m"},
				{Offset: "180d", Interval: "1h"},
			}}},
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
			Rules: []DownsamplingRule{{Periods: []DownsamplingPeriod{{Offset: "1d", Interval: "7m"}}}},
		},
	}, true)

	// downsampling - period interval not a multiple of dedupInterval
	f(VMClusterSpec{
		License: testLicense,
		Downsampling: &DownsamplingConfig{
			Rules:         []DownsamplingRule{{Periods: []DownsamplingPeriod{{Offset: "30d", Interval: "10m"}}}},
			DedupInterval: "7m",
		},
	}, true)

	// downsampling - period interval is a multiple of dedupInterval
	f(VMClusterSpec{
		License: testLicense,
		Downsampling: &DownsamplingConfig{
			Rules:         []DownsamplingRule{{Periods: []DownsamplingPeriod{{Offset: "30d", Interval: "10m"}}}},
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
}

func TestVMCluster_PrefixedName(t *testing.T) {
	f := func(name string, kind ClusterComponent, want string) {
		t.Helper()
		cr := &VMCluster{}
		cr.Name = name
		assert.Equal(t, want, cr.PrefixedName(kind))
	}

	f("myapp", ClusterComponentSelect, "vmselect-myapp")
	f("myapp", ClusterComponentInsert, "vminsert-myapp")
	f("myapp", ClusterComponentStorage, "vmstorage-myapp")
}
