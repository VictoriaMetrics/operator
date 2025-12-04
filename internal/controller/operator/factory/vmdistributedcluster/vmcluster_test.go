package vmdistributedcluster

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func TestMergeMapsRecursive_BasicAndNil(t *testing.T) {
	base := map[string]interface{}{
		"a": map[string]interface{}{
			"b": "keep",
			"c": "override-me",
		},
		"d": "root-keep",
	}
	override := map[string]interface{}{
		"a": map[string]interface{}{
			"c": "new",
			"z": "added",
		},
		"e": "root-added",
	}
	// initial merge
	modified := mergeMapsRecursive(base, override)
	assert.True(t, modified)
	assert.Equal(t, "keep", base["a"].(map[string]interface{})["b"])
	assert.Equal(t, "new", base["a"].(map[string]interface{})["c"])
	assert.Equal(t, "added", base["a"].(map[string]interface{})["z"])
	assert.Equal(t, "root-keep", base["d"])
	assert.Equal(t, "root-added", base["e"])

	// nil override removes keys
	override2 := map[string]interface{}{
		"a": map[string]interface{}{
			"z": nil,
		},
		"d": nil,
	}
	modified2 := mergeMapsRecursive(base, override2)
	assert.True(t, modified2)
	_, ok := base["a"].(map[string]interface{})["z"]
	assert.False(t, ok)
	_, ok = base["d"]
	assert.False(t, ok)
}

func TestMergeVMClusterSpecs_DeepMerge(t *testing.T) {
	base := vmv1beta1.VMClusterSpec{
		ClusterVersion: "v1.0.0",
		VMSelect: &vmv1beta1.VMSelect{
			CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
				ReplicaCount: ptrTo(int32(1)),
				ExtraArgs:    map[string]string{"keep": "x", "override": "old"},
			},
		},
		VMInsert: &vmv1beta1.VMInsert{
			CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
				ReplicaCount: ptrTo(int32(1)),
				ExtraArgs:    map[string]string{"insert-arg": "1"},
			},
		},
		ServiceAccountName: "base-sa",
	}
	zone := vmv1beta1.VMClusterSpec{
		ClusterVersion: "v1.2.3",
		VMSelect: &vmv1beta1.VMSelect{
			CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
				ReplicaCount: ptrTo(int32(3)),
				ExtraArgs:    map[string]string{"override": "new", "add": "y"},
			},
		},
		ServiceAccountName: "zone-sa",
	}

	merged, modified, err := mergeVMClusterSpecs(base, zone)
	assert.NoError(t, err)
	assert.True(t, modified)

	// top-level
	assert.Equal(t, "v1.2.3", merged.ClusterVersion)
	assert.Equal(t, "zone-sa", merged.ServiceAccountName)

	// nested merge
	if merged.VMSelect == nil || merged.VMSelect.ReplicaCount == nil {
		t.Fatalf("merged.VMSelect or ReplicaCount is nil")
	}
	assert.Equal(t, int32(3), *merged.VMSelect.ReplicaCount)
	assert.Equal(t, "x", merged.VMSelect.ExtraArgs["keep"])
	assert.Equal(t, "new", merged.VMSelect.ExtraArgs["override"])
	assert.Equal(t, "y", merged.VMSelect.ExtraArgs["add"])

	// untouched subtree
	if merged.VMInsert == nil || merged.VMInsert.ReplicaCount == nil {
		t.Fatalf("merged.VMInsert or ReplicaCount is nil")
	}
	assert.Equal(t, int32(1), *merged.VMInsert.ReplicaCount)
	assert.Equal(t, "1", merged.VMInsert.ExtraArgs["insert-arg"])
}

func TestApplyOverrideSpec_NilOverride(t *testing.T) {
	base := vmv1beta1.VMClusterSpec{
		ClusterVersion:     "v1.0.0",
		ServiceAccountName: "base",
	}
	merged, modified, err := ApplyOverrideSpec(base, nil)
	assert.NoError(t, err)
	assert.False(t, modified)
	assert.Equal(t, base, merged)

	// empty JSON override should be no-op
	empty := &apiextensionsv1.JSON{Raw: []byte("{}")}
	merged2, modified2, err := ApplyOverrideSpec(base, empty)
	assert.NoError(t, err)
	assert.False(t, modified2)
	assert.Equal(t, base, merged2)
}

func TestSetOwnerRefIfNeeded(t *testing.T) {
	// scheme with our types
	scheme := runtime.NewScheme()
	_ = vmv1alpha1.AddToScheme(scheme)
	_ = vmv1beta1.AddToScheme(scheme)

	cr := &vmv1alpha1.VMDistributedCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: vmv1alpha1.GroupVersion.String(),
			Kind:       "VMDistributedCluster",
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
	cr := &vmv1alpha1.VMDistributedCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: vmv1alpha1.GroupVersion.String(),
			Kind:       "VMDistributedCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vdc",
			Namespace: "default",
			UID:       k8stypes.UID("owner-uid"),
		},
	}
	otherCR := &vmv1alpha1.VMDistributedCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: vmv1alpha1.GroupVersion.String(),
			Kind:       "VMDistributedCluster",
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

	err := ensureNoVMClusterOwners(cr, vmc)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected owner reference")
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

func TestGetReferencedVMCluster(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = vmv1beta1.AddToScheme(scheme)

	existing := &vmv1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "exists",
			Namespace: "default",
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()

	// ok
	got, err := getReferencedVMCluster(context.Background(), cl, "default", &corev1.LocalObjectReference{Name: "exists"})
	assert.NoError(t, err)
	assert.Equal(t, "exists", got.Name)

	// not found
	_, err = getReferencedVMCluster(context.Background(), cl, "default", &corev1.LocalObjectReference{Name: "missing"})
	assert.Error(t, err)
	assert.True(t, k8serrors.IsNotFound(err) || contains(err.Error(), "not found"))
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
	zones := []vmv1alpha1.VMClusterRefOrSpec{
		{Ref: &corev1.LocalObjectReference{Name: "ref"}},
		{Name: "inline", Spec: &inlineSpec},
	}

	got, err := fetchVMClusters(context.Background(), cl, "ns", zones)
	assert.NoError(t, err)
	assert.Len(t, got, 2)
	assert.Equal(t, "ref", got[0].Name)
	assert.Equal(t, "inline", got[1].Name)
	assert.Equal(t, inlineSpec.ClusterVersion, got[1].Spec.ClusterVersion)
}

func TestValidateVMClusterRefOrSpec_Matrix(t *testing.T) {
	ok := vmv1alpha1.VMClusterRefOrSpec{Ref: &corev1.LocalObjectReference{Name: "a"}}
	assert.NoError(t, validateVMClusterRefOrSpec(0, ok))

	ok2 := vmv1alpha1.VMClusterRefOrSpec{Name: "b", Spec: &vmv1beta1.VMClusterSpec{}}
	assert.NoError(t, validateVMClusterRefOrSpec(1, ok2))

	both := vmv1alpha1.VMClusterRefOrSpec{
		Name: "c",
		Ref:  &corev1.LocalObjectReference{Name: "c"},
		Spec: &vmv1beta1.VMClusterSpec{},
	}
	assert.Error(t, validateVMClusterRefOrSpec(2, both))

	none := vmv1alpha1.VMClusterRefOrSpec{}
	assert.Error(t, validateVMClusterRefOrSpec(3, none))

	missingRefName := vmv1alpha1.VMClusterRefOrSpec{Ref: &corev1.LocalObjectReference{}}
	assert.Error(t, validateVMClusterRefOrSpec(4, missingRefName))

	missingSpecName := vmv1alpha1.VMClusterRefOrSpec{Spec: &vmv1beta1.VMClusterSpec{}}
	assert.Error(t, validateVMClusterRefOrSpec(5, missingSpecName))

	// Spec with OverrideSpec simultaneously
	overrideJSON, _ := json.Marshal(vmv1beta1.VMClusterSpec{})
	bad := vmv1alpha1.VMClusterRefOrSpec{
		Name:         "x",
		Spec:         &vmv1beta1.VMClusterSpec{},
		OverrideSpec: &apiextensionsv1.JSON{Raw: overrideJSON},
	}
	assert.Error(t, validateVMClusterRefOrSpec(6, bad))
}

func TestApplyGlobalOverrideSpec(t *testing.T) {
	base := vmv1beta1.VMClusterSpec{
		ClusterVersion:     "v1.0.0",
		ServiceAccountName: "base",
		RetentionPeriod:    "30d",
	}

	// Test with nil GlobalOverrideSpec
	globalOverride := (*apiextensionsv1.JSON)(nil)
	merged, modified, err := ApplyOverrideSpec(base, globalOverride)
	assert.NoError(t, err)
	assert.False(t, modified)
	assert.Equal(t, base, merged)

	// Test with empty GlobalOverrideSpec
	emptyGlobal := &apiextensionsv1.JSON{Raw: []byte("{}")}
	merged2, modified2, err := ApplyOverrideSpec(base, emptyGlobal)
	assert.NoError(t, err)
	assert.False(t, modified2)
	assert.Equal(t, base, merged2)

	// Test with GlobalOverrideSpec that modifies top-level fields
	globalTopLevel := &apiextensionsv1.JSON{Raw: []byte(`{"clusterVersion": "v2.0.0", "serviceAccountName": "global-sa"}`)}
	merged3, modified3, err := ApplyOverrideSpec(base, globalTopLevel)
	assert.NoError(t, err)
	assert.True(t, modified3)
	assert.Equal(t, "v2.0.0", merged3.ClusterVersion)
	assert.Equal(t, "global-sa", merged3.ServiceAccountName)
	assert.Equal(t, "30d", merged3.RetentionPeriod) // Unchanged field

	// Test with GlobalOverrideSpec that sets a field to null
	globalNullify := &apiextensionsv1.JSON{Raw: []byte(`{"serviceAccountName": null}`)}
	merged5, modified5, err := ApplyOverrideSpec(base, globalNullify)
	assert.NoError(t, err)
	assert.True(t, modified5)
	assert.Equal(t, "", merged5.ServiceAccountName) // Should be empty string (null in JSON becomes empty string after unmarshal)
}

func TestApplyGlobalAndClusterSpecificOverrideSpecs(t *testing.T) {
	base := vmv1beta1.VMClusterSpec{
		ClusterVersion:     "v1.0.0",
		ServiceAccountName: "base",
		RetentionPeriod:    "30d",
	}

	// Apply GlobalOverrideSpec first
	globalOverride := &apiextensionsv1.JSON{Raw: []byte(`{"clusterVersion": "v2.0.0", "serviceAccountName": "global-sa"}`)}
	merged1, modified1, err := ApplyOverrideSpec(base, globalOverride)
	assert.NoError(t, err)
	assert.True(t, modified1)
	assert.Equal(t, "v2.0.0", merged1.ClusterVersion)
	assert.Equal(t, "global-sa", merged1.ServiceAccountName)
	assert.Equal(t, "30d", merged1.RetentionPeriod)

	// Then apply cluster-specific override
	clusterOverride := &apiextensionsv1.JSON{Raw: []byte(`{"retentionPeriod": "10d", "clusterVersion": "v3.0.0"}`)}
	merged2, modified2, err := ApplyOverrideSpec(merged1, clusterOverride)
	assert.NoError(t, err)
	assert.True(t, modified2)
	assert.Equal(t, "v3.0.0", merged2.ClusterVersion)        // Cluster-specific override should take precedence
	assert.Equal(t, "global-sa", merged2.ServiceAccountName) // From global override, unchanged by cluster override
	assert.Equal(t, "10d", merged2.RetentionPeriod)          // From cluster override
}

// helpers
func ptrTo[T any](v T) *T { return &v }

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || (len(sub) > 0 && (indexOf(s, sub) >= 0)))
}

func indexOf(s, sub string) int {
outer:
	for i := 0; i+len(sub) <= len(s); i++ {
		for j := 0; j < len(sub); j++ {
			if s[i+j] != sub[j] {
				continue outer
			}
		}
		return i
	}
	return -1
}
