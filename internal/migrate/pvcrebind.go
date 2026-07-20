package migrate

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Polling intervals/timeouts are vars, not consts, so tests can shrink them.
var (
	PVCDeletePollInterval = 2 * time.Second
	PVCDeleteTimeout      = 2 * time.Minute
	PVCBindPollInterval   = 2 * time.Second
	PVCBindTimeout        = 2 * time.Minute
)

// RebindPVC adopts the PersistentVolume bound to sourcePVC under a new PVC named
// targetName/targetNamespace, so a differently-named workload can claim the same storage
// without copying data. The caller must ensure sourcePVC is already unmounted (its owning
// workload deleted) before calling this.
func RebindPVC(ctx context.Context, c client.Client, sourcePVC *corev1.PersistentVolumeClaim, targetName, targetNamespace string) (*corev1.PersistentVolumeClaim, error) {
	if sourcePVC.Spec.VolumeName == "" {
		return nil, fmt.Errorf("source PVC %s/%s has no bound PersistentVolume (spec.volumeName is empty)", sourcePVC.Namespace, sourcePVC.Name)
	}
	pvName := sourcePVC.Spec.VolumeName

	var pv corev1.PersistentVolume
	if err := c.Get(ctx, types.NamespacedName{Name: pvName}, &pv); err != nil {
		return nil, fmt.Errorf("cannot get PersistentVolume %q bound to PVC %s/%s: %w", pvName, sourcePVC.Namespace, sourcePVC.Name, err)
	}

	// Ensure the PV survives deleting the old PVC.
	if pv.Spec.PersistentVolumeReclaimPolicy != corev1.PersistentVolumeReclaimRetain {
		pv.Spec.PersistentVolumeReclaimPolicy = corev1.PersistentVolumeReclaimRetain
		if err := c.Update(ctx, &pv); err != nil {
			return nil, fmt.Errorf("cannot patch PersistentVolume %q to Retain reclaim policy: %w", pvName, err)
		}
	}

	// Check whether the desired target PVC already exists (safe to re-run this step).
	var existing corev1.PersistentVolumeClaim
	err := c.Get(ctx, types.NamespacedName{Name: targetName, Namespace: targetNamespace}, &existing)
	switch {
	case err == nil:
		return &existing, nil
	case !k8serrors.IsNotFound(err):
		return nil, fmt.Errorf("cannot check for existing target PVC %s/%s: %w", targetNamespace, targetName, err)
	}

	// Delete the source PVC and wait for it to actually disappear.
	if sourcePVC.Name != targetName || sourcePVC.Namespace != targetNamespace {
		if err := c.Delete(ctx, sourcePVC); err != nil && !k8serrors.IsNotFound(err) {
			return nil, fmt.Errorf("cannot delete source PVC %s/%s: %w", sourcePVC.Namespace, sourcePVC.Name, err)
		}
		if err := wait.PollUntilContextTimeout(ctx, PVCDeletePollInterval, PVCDeleteTimeout, true, func(ctx context.Context) (bool, error) {
			var check corev1.PersistentVolumeClaim
			getErr := c.Get(ctx, types.NamespacedName{Name: sourcePVC.Name, Namespace: sourcePVC.Namespace}, &check)
			return k8serrors.IsNotFound(getErr), nil
		}); err != nil {
			return nil, fmt.Errorf("timed out waiting for source PVC %s/%s to be deleted: %w", sourcePVC.Namespace, sourcePVC.Name, err)
		}
	}

	// Clear the claimRef so the PV returns to Available and can be claimed by the new PVC.
	if err := c.Get(ctx, types.NamespacedName{Name: pvName}, &pv); err != nil {
		return nil, fmt.Errorf("cannot re-fetch PersistentVolume %q: %w", pvName, err)
	}
	if pv.Spec.ClaimRef != nil {
		pv.Spec.ClaimRef = nil
		if err := c.Update(ctx, &pv); err != nil {
			return nil, fmt.Errorf("cannot clear claimRef on PersistentVolume %q: %w", pvName, err)
		}
	}

	target := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      targetName,
			Namespace: targetNamespace,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      sourcePVC.Spec.AccessModes,
			Resources:        sourcePVC.Spec.Resources,
			StorageClassName: sourcePVC.Spec.StorageClassName,
			VolumeName:       pvName,
		},
	}
	if err := c.Create(ctx, target); err != nil {
		return nil, fmt.Errorf("cannot create target PVC %s/%s bound to PersistentVolume %q: %w", targetNamespace, targetName, pvName, err)
	}

	if err := wait.PollUntilContextTimeout(ctx, PVCBindPollInterval, PVCBindTimeout, true, func(ctx context.Context) (bool, error) {
		var check corev1.PersistentVolumeClaim
		if err := c.Get(ctx, types.NamespacedName{Name: targetName, Namespace: targetNamespace}, &check); err != nil {
			return false, nil
		}
		target = &check
		return check.Status.Phase == corev1.ClaimBound, nil
	}); err != nil {
		return nil, fmt.Errorf("timed out waiting for target PVC %s/%s to bind to PersistentVolume %q: %w", targetNamespace, targetName, pvName, err)
	}

	return target, nil
}

// WaitPVCBound polls nsn's PVC until it reaches Bound phase.
func WaitPVCBound(ctx context.Context, c client.Client, nsn types.NamespacedName) error {
	err := wait.PollUntilContextTimeout(ctx, PVCBindPollInterval, PVCBindTimeout, true, func(ctx context.Context) (bool, error) {
		var pvc corev1.PersistentVolumeClaim
		if err := c.Get(ctx, nsn, &pvc); err != nil {
			return false, nil
		}
		return pvc.Status.Phase == corev1.ClaimBound, nil
	})
	if err != nil {
		return fmt.Errorf("timed out waiting for PVC %s to bind: %w", nsn.String(), err)
	}
	return nil
}
