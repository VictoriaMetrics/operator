package vmdistributed

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func TestMergeMapsRecursive_BasicAndNil(t *testing.T) {
	base := map[string]any{
		"a": map[string]any{
			"b": "keep",
			"c": "override-me",
		},
		"d": "root-keep",
	}
	override := map[string]any{
		"a": map[string]any{
			"c": "new",
			"z": "added",
		},
		"e": "root-added",
	}
	// initial merge
	modified := mergeMapsRecursive(base, override)
	assert.True(t, modified)
	assert.Equal(t, "keep", base["a"].(map[string]any)["b"])
	assert.Equal(t, "new", base["a"].(map[string]any)["c"])
	assert.Equal(t, "added", base["a"].(map[string]any)["z"])
	assert.Equal(t, "root-keep", base["d"])
	assert.Equal(t, "root-added", base["e"])
}

func TestMergeVMClusterSpecs_DeepMerge(t *testing.T) {

	base := &vmv1beta1.VMClusterSpec{
		ClusterVersion: "v1.0.0",
		VMSelect: &vmv1beta1.VMSelect{
			CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
				ReplicaCount: ptr.To(int32(1)),
				ExtraArgs:    map[string]string{"keep": "x", "override": "old"},
			},
		},
		VMInsert: &vmv1beta1.VMInsert{
			CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
				ReplicaCount: ptr.To(int32(1)),
				ExtraArgs:    map[string]string{"insert-arg": "1"},
			},
		},
		ServiceAccountName: "base-sa",
	}
	zone := &vmv1beta1.VMClusterSpec{
		ClusterVersion: "v1.2.3",
		VMSelect: &vmv1beta1.VMSelect{
			CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
				ReplicaCount: ptr.To(int32(3)),
				ExtraArgs:    map[string]string{"override": "new", "add": "y"},
			},
		},
		ServiceAccountName: "zone-sa",
	}

	modified, err := mergeDeep(base, zone)
	assert.NoError(t, err)
	assert.True(t, modified)

	// top-level
	assert.Equal(t, "v1.2.3", base.ClusterVersion)
	assert.Equal(t, "zone-sa", base.ServiceAccountName)

	// nested merge
	if base.VMSelect == nil || base.VMSelect.ReplicaCount == nil {
		t.Fatalf("merged.VMSelect or ReplicaCount is nil")
	}
	assert.Equal(t, int32(3), *base.VMSelect.ReplicaCount)
	assert.Equal(t, "x", base.VMSelect.ExtraArgs["keep"])
	assert.Equal(t, "new", base.VMSelect.ExtraArgs["override"])
	assert.Equal(t, "y", base.VMSelect.ExtraArgs["add"])

	// untouched subtree
	if base.VMInsert == nil || base.VMInsert.ReplicaCount == nil {
		t.Fatalf("base.VMInsert or ReplicaCount is nil")
	}
	assert.Equal(t, int32(1), *base.VMInsert.ReplicaCount)
	assert.Equal(t, "1", base.VMInsert.ExtraArgs["insert-arg"])
}

func TestApplyOverrideSpec_NilOverride(t *testing.T) {
	base := &vmv1beta1.VMClusterSpec{
		ClusterVersion:     "v1.0.0",
		ServiceAccountName: "base",
	}
	merged := base.DeepCopy()
	modified, err := mergeDeep(merged, nil)
	assert.NoError(t, err)
	assert.False(t, modified)
	assert.Equal(t, base, merged)
}

func TestSetOwnerRefIfNeeded(t *testing.T) {
	// scheme with our types
	scheme := runtime.NewScheme()
	_ = vmv1alpha1.AddToScheme(scheme)
	_ = vmv1beta1.AddToScheme(scheme)

	cr := &vmv1alpha1.VMDistributed{
		TypeMeta: metav1.TypeMeta{
			APIVersion: vmv1alpha1.GroupVersion.String(),
			Kind:       "VMDistributed",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vdc",
			Namespace: "default",
			UID:       k8stypes.UID("owner-uid"),
		},
	}
	vmc := &vmv1beta1.VMCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: vmv1beta1.GroupVersion.String(),
			Kind:       "VMCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vmc",
			Namespace: "default",
		},
	}

	modified, err := setOwnerRefIfNeeded(cr, vmc, scheme)
	assert.NoError(t, err)
	assert.True(t, modified)
	assert.Len(t, vmc.OwnerReferences, 1)
	assert.Equal(t, "vdc", vmc.OwnerReferences[0].Name)

	// second call should detect owner ref already set
	modified2, err := setOwnerRefIfNeeded(cr, vmc, scheme)
	assert.NoError(t, err)
	assert.False(t, modified2)
}

func TestEnsureNoVMClusterOwners(t *testing.T) {
	cr := &vmv1alpha1.VMDistributed{
		TypeMeta: metav1.TypeMeta{
			APIVersion: vmv1alpha1.GroupVersion.String(),
			Kind:       "VMDistributed",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vdc",
			Namespace: "default",
			UID:       k8stypes.UID("owner-uid"),
		},
	}
	otherCR := &vmv1alpha1.VMDistributed{
		TypeMeta: metav1.TypeMeta{
			APIVersion: vmv1alpha1.GroupVersion.String(),
			Kind:       "VMDistributed",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "other",
			Namespace: "default",
			UID:       k8stypes.UID("other-uid"),
		},
	}

	vmc := &vmv1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vmc",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: otherCR.APIVersion,
					Kind:       otherCR.Kind,
					Name:       otherCR.Name,
					UID:        otherCR.UID,
				},
			},
		},
	}

	err := cr.Owns(vmc)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "is owned by other distributed resource")
}

func TestWaitForVMClusterReady_Success(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = vmv1beta1.AddToScheme(scheme)

	vmc := &vmv1beta1.VMCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: vmv1beta1.GroupVersion.String(),
			Kind:       "VMCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vmc",
			Namespace: "default",
		},
		Status: vmv1beta1.VMClusterStatus{
			StatusMetadata: vmv1beta1.StatusMetadata{
				UpdateStatus: vmv1beta1.UpdateStatusOperational,
			},
		},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(vmc).Build()
	ctx := context.Background()
	err := waitForVMClusterReady(ctx, cl, vmc.DeepCopy(), 500*time.Millisecond)
	assert.NoError(t, err)
}

func TestWaitForVMClusterReady_Timeout(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = vmv1beta1.AddToScheme(scheme)

	vmc := &vmv1beta1.VMCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: vmv1beta1.GroupVersion.String(),
			Kind:       "VMCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vmc-stuck",
			Namespace: "default",
		},
		Status: vmv1beta1.VMClusterStatus{
			StatusMetadata: vmv1beta1.StatusMetadata{
				UpdateStatus: vmv1beta1.UpdateStatusExpanding,
			},
		},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(vmc).Build()
	ctx := context.Background()
	err := waitForVMClusterReady(ctx, cl, vmc.DeepCopy(), 100*time.Millisecond)
	assert.Error(t, err)
}

func TestFetchVMClusters_InlineAndRef(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = vmv1alpha1.AddToScheme(scheme)
	_ = vmv1beta1.AddToScheme(scheme)

	refCluster := &vmv1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ref",
			Namespace: "ns",
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(refCluster).Build()

	inlineSpec := vmv1beta1.VMClusterSpec{
		ClusterVersion: "v0.1.0",
	}
	cr := &vmv1alpha1.VMDistributed{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "ns",
		},
		Spec: vmv1alpha1.VMDistributedSpec{
			Zones: []vmv1alpha1.VMDistributedZone{
				{
					VMCluster: &vmv1alpha1.VMClusterObjOrRef{
						Ref: &corev1.LocalObjectReference{Name: "ref"},
					},
				},
				{
					VMCluster: &vmv1alpha1.VMClusterObjOrRef{
						Name: "inline",
						Spec: &inlineSpec,
					},
				},
			},
		},
	}

	got, err := fetchVMClusters(context.Background(), cl, cr)
	assert.NoError(t, err)
	assert.Len(t, got, 2)
	assert.Equal(t, "ref", got[0].Name)
	assert.Equal(t, "inline", got[1].Name)
	assert.Equal(t, inlineSpec.ClusterVersion, got[1].Spec.ClusterVersion)
}

func TestFetchVMClusters_SortedByGeneration(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = vmv1alpha1.AddToScheme(scheme)
	_ = vmv1beta1.AddToScheme(scheme)

	c1 := &vmv1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster-1", Namespace: "ns"},
		Status:     vmv1beta1.VMClusterStatus{StatusMetadata: vmv1beta1.StatusMetadata{ObservedGeneration: 1}},
	}
	c2 := &vmv1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster-2", Namespace: "ns"},
		Status:     vmv1beta1.VMClusterStatus{StatusMetadata: vmv1beta1.StatusMetadata{ObservedGeneration: 3}},
	}
	c3 := &vmv1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster-3", Namespace: "ns"},
		Status:     vmv1beta1.VMClusterStatus{StatusMetadata: vmv1beta1.StatusMetadata{ObservedGeneration: 2}},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(c1, c2, c3).Build()

	cr := &vmv1alpha1.VMDistributed{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "ns",
		},
		Spec: vmv1alpha1.VMDistributedSpec{
			Zones: []vmv1alpha1.VMDistributedZone{
				{
					VMCluster: &vmv1alpha1.VMClusterObjOrRef{
						Ref: &corev1.LocalObjectReference{Name: "cluster-1"},
					},
				},
				{
					VMCluster: &vmv1alpha1.VMClusterObjOrRef{
						Ref: &corev1.LocalObjectReference{Name: "cluster-2"},
					},
				},
				{
					VMCluster: &vmv1alpha1.VMClusterObjOrRef{
						Ref: &corev1.LocalObjectReference{Name: "cluster-3"},
					},
				},
			},
		},
	}
	got, err := fetchVMClusters(context.Background(), cl, cr)
	assert.NoError(t, err)
	assert.Len(t, got, 3)

	// Expected order: descending by ObservedGeneration
	assert.Equal(t, "cluster-2", got[0].Name)
	assert.Equal(t, int64(3), got[0].Status.ObservedGeneration)
	assert.Equal(t, "cluster-3", got[1].Name)
	assert.Equal(t, int64(2), got[1].Status.ObservedGeneration)
	assert.Equal(t, "cluster-1", got[2].Name)
	assert.Equal(t, int64(1), got[2].Status.ObservedGeneration)
}

func TestDeepMerge(t *testing.T) {
	type opts struct {
		override *vmv1beta1.VMClusterSpec
		validate func(base, merged *vmv1beta1.VMClusterSpec, modified bool, err error)
	}
	f := func(oss ...opts) {
		t.Helper()
		base := &vmv1beta1.VMClusterSpec{
			ClusterVersion:     "v1.0.0",
			ServiceAccountName: "base",
			RetentionPeriod:    "30d",
		}
		merged := base.DeepCopy()
		for _, o := range oss {
			modified, err := mergeDeep(merged, o.override)
			o.validate(base, merged, modified, err)
		}
	}

	// Test with nil override spec
	f(opts{
		validate: func(base, merged *vmv1beta1.VMClusterSpec, modified bool, err error) {
			assert.NoError(t, err)
			assert.False(t, modified)
			assert.Equal(t, base, merged)
		},
	})

	// Test with empty override spec
	f(opts{
		override: &vmv1beta1.VMClusterSpec{},
		validate: func(base, merged *vmv1beta1.VMClusterSpec, modified bool, err error) {
			assert.NoError(t, err)
			assert.False(t, modified)
			assert.Equal(t, base, merged)
		},
	})

	// Test with override spec that modifies top-level fields
	f(opts{
		override: &vmv1beta1.VMClusterSpec{
			ClusterVersion:     "v2.0.0",
			ServiceAccountName: "global-sa",
		},
		validate: func(_, merged *vmv1beta1.VMClusterSpec, modified bool, err error) {
			assert.True(t, modified)
			assert.Equal(t, "v2.0.0", merged.ClusterVersion)
			assert.Equal(t, "global-sa", merged.ServiceAccountName)
			assert.Equal(t, "30d", merged.RetentionPeriod)
		},
	})

	// multiple overrides
	f(opts{
		override: &vmv1beta1.VMClusterSpec{
			ClusterVersion:     "v2.0.0",
			ServiceAccountName: "global-sa",
		},
		validate: func(_, merged *vmv1beta1.VMClusterSpec, modified bool, err error) {
			assert.NoError(t, err)
			assert.True(t, modified)
			assert.Equal(t, "v2.0.0", merged.ClusterVersion)
			assert.Equal(t, "global-sa", merged.ServiceAccountName)
			assert.Equal(t, "30d", merged.RetentionPeriod)
		},
	}, opts{
		override: &vmv1beta1.VMClusterSpec{
			RetentionPeriod: "10d",
			ClusterVersion:  "v3.0.0",
		},
		validate: func(_, merged *vmv1beta1.VMClusterSpec, modified bool, err error) {
			assert.NoError(t, err)
			assert.True(t, modified)
			assert.Equal(t, "v3.0.0", merged.ClusterVersion)        // Cluster-specific override should take precedence
			assert.Equal(t, "global-sa", merged.ServiceAccountName) // From global override, unchanged by cluster override
			assert.Equal(t, "10d", merged.RetentionPeriod)          // From cluster override
		},
	})
}
