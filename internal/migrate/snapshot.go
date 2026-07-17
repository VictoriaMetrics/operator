package migrate

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
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
// be empty to use the cluster's default VolumeSnapshotClass. A same-named snapshot already
// existing (e.g. left over from an earlier failed attempt, during which the source PVC may
// have gone on accepting writes normally again after a revert) is never reused as-is: it's
// deleted and replaced with a fresh one, so a retry can't silently restore from stale data.
func CreateVolumeSnapshot(ctx context.Context, c client.Client, namespace, name, sourcePVCName, snapshotClassName string) error {
	spec := map[string]any{
		"source": map[string]any{
			"persistentVolumeClaimName": sourcePVCName,
		},
	}
	if snapshotClassName != "" {
		spec["volumeSnapshotClassName"] = snapshotClassName
	}
	newSnapshot := func() (*unstructured.Unstructured, error) {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(VolumeSnapshotGVK)
		obj.SetName(name)
		obj.SetNamespace(namespace)
		if err := unstructured.SetNestedMap(obj.Object, spec, "spec"); err != nil {
			return nil, fmt.Errorf("cannot build VolumeSnapshot spec: %w", err)
		}
		return obj, nil
	}

	obj, err := newSnapshot()
	if err != nil {
		return err
	}
	if err := c.Create(ctx, obj); err != nil {
		if !k8serrors.IsAlreadyExists(err) {
			return fmt.Errorf("cannot create VolumeSnapshot %s/%s (is the snapshot.storage.k8s.io CSI CRD installed in this cluster?): %w", namespace, name, err)
		}
		if err := c.Delete(ctx, obj); err != nil && !k8serrors.IsNotFound(err) {
			return fmt.Errorf("cannot delete stale VolumeSnapshot %s/%s: %w", namespace, name, err)
		}
		if err := wait.PollUntilContextTimeout(ctx, SnapshotPollInterval, SnapshotTimeout, true, func(ctx context.Context) (bool, error) {
			check := &unstructured.Unstructured{}
			check.SetGroupVersionKind(VolumeSnapshotGVK)
			getErr := c.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, check)
			if getErr == nil {
				return false, nil
			}
			if k8serrors.IsNotFound(getErr) {
				return true, nil
			}
			return false, getErr
		}); err != nil {
			return fmt.Errorf("waiting for stale VolumeSnapshot %s/%s to be deleted: %w", namespace, name, err)
		}
		fresh, err := newSnapshot()
		if err != nil {
			return err
		}
		if err := c.Create(ctx, fresh); err != nil {
			return fmt.Errorf("cannot create VolumeSnapshot %s/%s: %w", namespace, name, err)
		}
	}
	return nil
}

// WaitVolumeSnapshotReady polls the VolumeSnapshot until status.readyToUse is true.
func WaitVolumeSnapshotReady(ctx context.Context, c client.Client, namespace, name string) error {
	err := wait.PollUntilContextTimeout(ctx, SnapshotPollInterval, SnapshotTimeout, true, func(ctx context.Context) (bool, error) {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(VolumeSnapshotGVK)
		if err := c.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, obj); err != nil {
			if k8serrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		ready, found, _ := unstructured.NestedBool(obj.Object, "status", "readyToUse")
		return found && ready, nil
	})
	if err != nil {
		return fmt.Errorf("waiting for VolumeSnapshot %s/%s to become ready: %w", namespace, name, err)
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
			VolumeMode:       source.Spec.VolumeMode,
			Resources:        source.Spec.Resources,
			DataSource: &corev1.TypedLocalObjectReference{
				APIGroup: &apiGroup,
				Kind:     "VolumeSnapshot",
				Name:     snapshotName,
			},
		},
	}
}
