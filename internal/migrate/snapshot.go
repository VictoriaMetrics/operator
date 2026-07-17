package migrate

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// VolumeSnapshotGVK identifies the CSI external-snapshotter VolumeSnapshot type. Handled via
// unstructured rather than a typed dependency, since those CRDs aren't guaranteed installed.
var VolumeSnapshotGVK = schema.GroupVersionKind{Group: "snapshot.storage.k8s.io", Version: "v1", Kind: "VolumeSnapshot"}

// Polling interval/timeout are vars, not consts, so tests can shrink them.
var (
	SnapshotPollInterval = 5 * time.Second
	SnapshotTimeout      = 15 * time.Minute
)

// CreateVolumeSnapshot creates a CSI VolumeSnapshot of sourcePVCName. snapshotClassName may
// be empty to use the cluster's default VolumeSnapshotClass.
func CreateVolumeSnapshot(ctx context.Context, c client.Client, namespace, name, sourcePVCName, snapshotClassName string) error {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(VolumeSnapshotGVK)
	obj.SetName(name)
	obj.SetNamespace(namespace)
	spec := map[string]any{
		"source": map[string]any{
			"persistentVolumeClaimName": sourcePVCName,
		},
	}
	if snapshotClassName != "" {
		spec["volumeSnapshotClassName"] = snapshotClassName
	}
	if err := unstructured.SetNestedMap(obj.Object, spec, "spec"); err != nil {
		return fmt.Errorf("cannot build VolumeSnapshot spec: %w", err)
	}
	if err := c.Create(ctx, obj); err != nil {
		return fmt.Errorf("cannot create VolumeSnapshot %s/%s (is the snapshot.storage.k8s.io CSI CRD installed in this cluster?): %w", namespace, name, err)
	}
	return nil
}

// WaitVolumeSnapshotReady polls the VolumeSnapshot until status.readyToUse is true.
func WaitVolumeSnapshotReady(ctx context.Context, c client.Client, namespace, name string) error {
	err := wait.PollUntilContextTimeout(ctx, SnapshotPollInterval, SnapshotTimeout, true, func(ctx context.Context) (bool, error) {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(VolumeSnapshotGVK)
		if err := c.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, obj); err != nil {
			return false, nil
		}
		ready, found, _ := unstructured.NestedBool(obj.Object, "status", "readyToUse")
		return found && ready, nil
	})
	if err != nil {
		return fmt.Errorf("timed out waiting for VolumeSnapshot %s/%s to become ready: %w", namespace, name, err)
	}
	return nil
}

// NewPVCFromSnapshot builds a PersistentVolumeClaim restored from an existing, ready
// VolumeSnapshot, copying access modes/storage class/size from source.
func NewPVCFromSnapshot(name, namespace, snapshotName string, source *corev1.PersistentVolumeClaim) *corev1.PersistentVolumeClaim {
	apiGroup := VolumeSnapshotGVK.Group
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      source.Spec.AccessModes,
			StorageClassName: source.Spec.StorageClassName,
			Resources:        source.Spec.Resources,
			DataSource: &corev1.TypedLocalObjectReference{
				APIGroup: &apiGroup,
				Kind:     "VolumeSnapshot",
				Name:     snapshotName,
			},
		},
	}
}
