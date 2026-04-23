package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
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

func TestVTCluster_FinalLabels(t *testing.T) {
	type opts struct {
		cr           *VTCluster
		commonLabels map[string]string
		want         map[string]string
	}
	f := func(o opts) {
		t.Helper()
		cfg := config.MustGetBaseConfig()
		orig := *cfg
		defer func() { *cfg = orig }()
		cfg.CommonLabels = o.commonLabels
		assert.Equal(t, o.want, o.cr.FinalLabels(vmv1beta1.ClusterComponentStorage))
	}

	// no common labels
	f(opts{
		cr: &VTCluster{ObjectMeta: metav1.ObjectMeta{Name: "test"}},
		want: map[string]string{
			"app.kubernetes.io/name":      "vtstorage",
			"app.kubernetes.io/instance":  "test",
			"app.kubernetes.io/component": "monitoring",
			"managed-by":                  "vm-operator",
			"app.kubernetes.io/part-of":   "vtcluster",
		},
	})
	// common labels added
	f(opts{
		cr:           &VTCluster{ObjectMeta: metav1.ObjectMeta{Name: "test"}},
		commonLabels: map[string]string{"team": "platform"},
		want: map[string]string{
			"app.kubernetes.io/name":      "vtstorage",
			"app.kubernetes.io/instance":  "test",
			"app.kubernetes.io/component": "monitoring",
			"managed-by":                  "vm-operator",
			"app.kubernetes.io/part-of":   "vtcluster",
			"team":                        "platform",
		},
	})
	// common labels cannot override existing
	f(opts{
		cr:           &VTCluster{ObjectMeta: metav1.ObjectMeta{Name: "test"}},
		commonLabels: map[string]string{"managed-by": "intruder", "team": "platform"},
		want: map[string]string{
			"app.kubernetes.io/name":      "vtstorage",
			"app.kubernetes.io/instance":  "test",
			"app.kubernetes.io/component": "monitoring",
			"managed-by":                  "vm-operator",
			"app.kubernetes.io/part-of":   "vtcluster",
			"team":                        "platform",
		},
	})
	// common labels cannot override managedMetadata
	f(opts{
		cr: &VTCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test"},
			Spec:       VTClusterSpec{ManagedMetadata: &vmv1beta1.ManagedObjectsMetadata{Labels: map[string]string{"team": "backend"}}},
		},
		commonLabels: map[string]string{"team": "intruder", "env": "prod"},
		want: map[string]string{
			"app.kubernetes.io/name":      "vtstorage",
			"app.kubernetes.io/instance":  "test",
			"app.kubernetes.io/component": "monitoring",
			"managed-by":                  "vm-operator",
			"app.kubernetes.io/part-of":   "vtcluster",
			"team":                        "backend",
			"env":                         "prod",
		},
	})
}

func TestVTCluster_FinalAnnotations(t *testing.T) {
	type opts struct {
		cr                *VTCluster
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
	f(opts{cr: &VTCluster{ObjectMeta: metav1.ObjectMeta{Name: "test"}}, want: nil})
	// common annotations added
	f(opts{
		cr:                &VTCluster{ObjectMeta: metav1.ObjectMeta{Name: "test"}},
		commonAnnotations: map[string]string{"note": "managed-by-gitops"},
		want:              map[string]string{"note": "managed-by-gitops"},
	})
	// common annotations cannot override managedMetadata
	f(opts{
		cr: &VTCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test"},
			Spec:       VTClusterSpec{ManagedMetadata: &vmv1beta1.ManagedObjectsMetadata{Annotations: map[string]string{"note": "from-spec"}}},
		},
		commonAnnotations: map[string]string{"note": "intruder", "extra": "value"},
		want:              map[string]string{"note": "from-spec", "extra": "value"},
	})
}

func TestVLCluster_FinalLabels(t *testing.T) {
	type opts struct {
		cr           *VLCluster
		commonLabels map[string]string
		want         map[string]string
	}
	f := func(o opts) {
		t.Helper()
		cfg := config.MustGetBaseConfig()
		orig := *cfg
		defer func() { *cfg = orig }()
		cfg.CommonLabels = o.commonLabels
		assert.Equal(t, o.want, o.cr.FinalLabels(vmv1beta1.ClusterComponentStorage))
	}

	// no common labels
	f(opts{
		cr: &VLCluster{ObjectMeta: metav1.ObjectMeta{Name: "test"}},
		want: map[string]string{
			"app.kubernetes.io/name":      "vlstorage",
			"app.kubernetes.io/instance":  "test",
			"app.kubernetes.io/component": "monitoring",
			"managed-by":                  "vm-operator",
			"app.kubernetes.io/part-of":   "vlcluster",
		},
	})
	// common labels added
	f(opts{
		cr:           &VLCluster{ObjectMeta: metav1.ObjectMeta{Name: "test"}},
		commonLabels: map[string]string{"team": "platform"},
		want: map[string]string{
			"app.kubernetes.io/name":      "vlstorage",
			"app.kubernetes.io/instance":  "test",
			"app.kubernetes.io/component": "monitoring",
			"managed-by":                  "vm-operator",
			"app.kubernetes.io/part-of":   "vlcluster",
			"team":                        "platform",
		},
	})
	// common labels cannot override existing
	f(opts{
		cr:           &VLCluster{ObjectMeta: metav1.ObjectMeta{Name: "test"}},
		commonLabels: map[string]string{"managed-by": "intruder", "team": "platform"},
		want: map[string]string{
			"app.kubernetes.io/name":      "vlstorage",
			"app.kubernetes.io/instance":  "test",
			"app.kubernetes.io/component": "monitoring",
			"managed-by":                  "vm-operator",
			"app.kubernetes.io/part-of":   "vlcluster",
			"team":                        "platform",
		},
	})
	// common labels cannot override managedMetadata
	f(opts{
		cr: &VLCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test"},
			Spec:       VLClusterSpec{ManagedMetadata: &vmv1beta1.ManagedObjectsMetadata{Labels: map[string]string{"team": "backend"}}},
		},
		commonLabels: map[string]string{"team": "intruder", "env": "prod"},
		want: map[string]string{
			"app.kubernetes.io/name":      "vlstorage",
			"app.kubernetes.io/instance":  "test",
			"app.kubernetes.io/component": "monitoring",
			"managed-by":                  "vm-operator",
			"app.kubernetes.io/part-of":   "vlcluster",
			"team":                        "backend",
			"env":                         "prod",
		},
	})
}

func TestVLCluster_FinalAnnotations(t *testing.T) {
	type opts struct {
		cr                *VLCluster
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
	f(opts{cr: &VLCluster{ObjectMeta: metav1.ObjectMeta{Name: "test"}}, want: nil})
	// common annotations added
	f(opts{
		cr:                &VLCluster{ObjectMeta: metav1.ObjectMeta{Name: "test"}},
		commonAnnotations: map[string]string{"note": "managed-by-gitops"},
		want:              map[string]string{"note": "managed-by-gitops"},
	})
	// common annotations cannot override managedMetadata
	f(opts{
		cr: &VLCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test"},
			Spec:       VLClusterSpec{ManagedMetadata: &vmv1beta1.ManagedObjectsMetadata{Annotations: map[string]string{"note": "from-spec"}}},
		},
		commonAnnotations: map[string]string{"note": "intruder", "extra": "value"},
		want:              map[string]string{"note": "from-spec", "extra": "value"},
	})
}
