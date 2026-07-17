package migrate

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// volumeSnapshotTestScheme registers the VolumeSnapshot GVK as an unstructured-compatible
// type, mimicking a cluster with the external-snapshotter CRDs installed, without taking a
// hard dependency on their generated Go types (see snapshot.go's package doc).
func volumeSnapshotTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	s.AddKnownTypeWithName(VolumeSnapshotGVK, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(VolumeSnapshotGVK.GroupVersion().WithKind("VolumeSnapshotList"), &unstructured.UnstructuredList{})
	return s
}

func TestCreateAndWaitVolumeSnapshot(t *testing.T) {
	SnapshotPollInterval = 10 * time.Millisecond
	SnapshotTimeout = time.Second

	c := fake.NewClientBuilder().WithScheme(volumeSnapshotTestScheme()).Build()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	require.NoError(t, CreateVolumeSnapshot(ctx, c, "default", "data-snap", "data-pvc", "csi-snap-class"))

	go func() {
		time.Sleep(30 * time.Millisecond)
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(VolumeSnapshotGVK)
		if err := c.Get(ctx, types.NamespacedName{Name: "data-snap", Namespace: "default"}, obj); err != nil {
			return
		}
		_ = unstructured.SetNestedField(obj.Object, true, "status", "readyToUse")
		_ = c.Update(ctx, obj)
	}()

	require.NoError(t, WaitVolumeSnapshotReady(ctx, c, "default", "data-snap"))
}

func TestNewPVCFromSnapshot(t *testing.T) {
	source := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "old-data", Namespace: "default"},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			StorageClassName: ptrTo("fast"),
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("10Gi")},
			},
		},
	}

	pvc := NewPVCFromSnapshot("vmsingle-newrelease", "default", "old-data-migration-snapshot", source)
	assert.Equal(t, "vmsingle-newrelease", pvc.Name)
	assert.Equal(t, source.Spec.AccessModes, pvc.Spec.AccessModes)
	assert.Equal(t, source.Spec.StorageClassName, pvc.Spec.StorageClassName)
	assert.Equal(t, source.Spec.Resources, pvc.Spec.Resources)
	require.NotNil(t, pvc.Spec.DataSource)
	assert.Equal(t, "VolumeSnapshot", pvc.Spec.DataSource.Kind)
	assert.Equal(t, "old-data-migration-snapshot", pvc.Spec.DataSource.Name)
	assert.Equal(t, "snapshot.storage.k8s.io", *pvc.Spec.DataSource.APIGroup)
}

func ptrTo[T any](v T) *T { return &v }
