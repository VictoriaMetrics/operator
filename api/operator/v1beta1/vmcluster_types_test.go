package v1beta1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/VictoriaMetrics/operator/internal/config"
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
	f := func(cr *VMCluster, requestsType string, want []int32) {
		t.Helper()
		assert.Equal(t, want, cr.AvailableStorageNodeIDs(requestsType))
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
	f(cr, "select", []int32{0, 2, 4})

	// insert excludes maintenance nodes
	f(cr, "insert", []int32{1, 2, 3})

	// no maintenance nodes
	f(&VMCluster{
		Spec: VMClusterSpec{
			VMStorage: &VMStorage{
				CommonAppsParams: CommonAppsParams{ReplicaCount: ptr.To(int32(3))},
			},
		},
	}, "select", []int32{0, 1, 2})
}

func TestVMCluster_FinalLabels(t *testing.T) {
	type opts struct {
		cr           *VMCluster
		commonLabels map[string]string
		want         map[string]string
	}
	f := func(o opts) {
		t.Helper()
		cfg := config.MustGetBaseConfig()
		orig := *cfg
		defer func() { *cfg = orig }()
		cfg.CommonLabels = o.commonLabels
		assert.Equal(t, o.want, o.cr.FinalLabels(ClusterComponentStorage))
	}

	// no common labels
	f(opts{
		cr: &VMCluster{ObjectMeta: metav1.ObjectMeta{Name: "test"}},
		want: map[string]string{
			"app.kubernetes.io/name":      "vmstorage",
			"app.kubernetes.io/instance":  "test",
			"app.kubernetes.io/component": "monitoring",
			"managed-by":                  "vm-operator",
			"app.kubernetes.io/part-of":   "vmcluster",
		},
	})
	// common labels added
	f(opts{
		cr:           &VMCluster{ObjectMeta: metav1.ObjectMeta{Name: "test"}},
		commonLabels: map[string]string{"team": "platform"},
		want: map[string]string{
			"app.kubernetes.io/name":      "vmstorage",
			"app.kubernetes.io/instance":  "test",
			"app.kubernetes.io/component": "monitoring",
			"managed-by":                  "vm-operator",
			"app.kubernetes.io/part-of":   "vmcluster",
			"team":                        "platform",
		},
	})
	// common labels cannot override existing
	f(opts{
		cr:           &VMCluster{ObjectMeta: metav1.ObjectMeta{Name: "test"}},
		commonLabels: map[string]string{"managed-by": "intruder", "team": "platform"},
		want: map[string]string{
			"app.kubernetes.io/name":      "vmstorage",
			"app.kubernetes.io/instance":  "test",
			"app.kubernetes.io/component": "monitoring",
			"managed-by":                  "vm-operator",
			"app.kubernetes.io/part-of":   "vmcluster",
			"team":                        "platform",
		},
	})
	// common labels cannot override managedMetadata
	f(opts{
		cr: &VMCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test"},
			Spec:       VMClusterSpec{ManagedMetadata: &ManagedObjectsMetadata{Labels: map[string]string{"team": "backend"}}},
		},
		commonLabels: map[string]string{"team": "intruder", "env": "prod"},
		want: map[string]string{
			"app.kubernetes.io/name":      "vmstorage",
			"app.kubernetes.io/instance":  "test",
			"app.kubernetes.io/component": "monitoring",
			"managed-by":                  "vm-operator",
			"app.kubernetes.io/part-of":   "vmcluster",
			"team":                        "backend",
			"env":                         "prod",
		},
	})
}

func TestVMCluster_FinalAnnotations(t *testing.T) {
	type opts struct {
		cr                *VMCluster
		commonAnnotations map[string]string
		want              map[string]string
	}
	f := func(o opts) {
		t.Helper()
		cfg := config.MustGetBaseConfig()
		orig := *cfg
		defer func() { *cfg = orig }()
		cfg.CommonAnnotations = o.commonAnnotations
		assert.Equal(t, o.want, o.cr.FinalAnnotations())
	}

	// no annotations
	f(opts{cr: &VMCluster{ObjectMeta: metav1.ObjectMeta{Name: "test"}}, want: nil})
	// common annotations added
	f(opts{
		cr:                &VMCluster{ObjectMeta: metav1.ObjectMeta{Name: "test"}},
		commonAnnotations: map[string]string{"note": "managed-by-gitops"},
		want:              map[string]string{"note": "managed-by-gitops"},
	})
	// common annotations cannot override managedMetadata
	f(opts{
		cr: &VMCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test"},
			Spec:       VMClusterSpec{ManagedMetadata: &ManagedObjectsMetadata{Annotations: map[string]string{"note": "from-spec"}}},
		},
		commonAnnotations: map[string]string{"note": "intruder", "extra": "value"},
		want:              map[string]string{"note": "from-spec", "extra": "value"},
	})
}
