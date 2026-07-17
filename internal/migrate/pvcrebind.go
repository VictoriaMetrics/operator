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
	if sourcePVC.Status.Phase != corev1.ClaimBound {
		return nil, fmt.Errorf("source PVC %s/%s is not Bound (phase=%q) — refusing to rebind an unbound claim", sourcePVC.Namespace, sourcePVC.Name, sourcePVC.Status.Phase)
	}
	pvName := sourcePVC.Spec.VolumeName

	var pv corev1.PersistentVolume
	if err := c.Get(ctx, types.NamespacedName{Name: pvName}, &pv); err != nil {
		return nil, fmt.Errorf("cannot get PersistentVolume %q bound to PVC %s/%s: %w", pvName, sourcePVC.Namespace, sourcePVC.Name, err)
	}
	originalReclaimPolicy := pv.Spec.PersistentVolumeReclaimPolicy

	// Ensure the PV survives deleting the old PVC; restored once the new PVC is bound.
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
		if existing.Spec.VolumeName != pvName {
			return nil, fmt.Errorf("target PVC %s/%s already exists but is bound to PersistentVolume %q, not %q — refusing to treat it as this rebind's target",
				targetNamespace, targetName, existing.Spec.VolumeName, pvName)
		}
		if existing.DeletionTimestamp != nil {
			return nil, fmt.Errorf("target PVC %s/%s already exists but is terminating — wait for it to finish deleting, or delete it manually, before retrying", targetNamespace, targetName)
		}
		if existing.Status.Phase != corev1.ClaimBound {
			bound, err := waitPVCBoundRef(ctx, c, types.NamespacedName{Name: targetName, Namespace: targetNamespace})
			if err != nil {
				return nil, err
			}
			existing = *bound
		}
		restorePVReclaimPolicy(ctx, c, pvName, originalReclaimPolicy)
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
			if getErr == nil {
				return false, nil
			}
			if k8serrors.IsNotFound(getErr) {
				return true, nil
			}
			return false, getErr
		}); err != nil {
			return nil, fmt.Errorf("waiting for source PVC %s/%s to be deleted: %w", sourcePVC.Namespace, sourcePVC.Name, err)
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
			AccessModes:               sourcePVC.Spec.AccessModes,
			Resources:                 sourcePVC.Spec.Resources,
			StorageClassName:          sourcePVC.Spec.StorageClassName,
			VolumeMode:                sourcePVC.Spec.VolumeMode,
			VolumeAttributesClassName: sourcePVC.Spec.VolumeAttributesClassName,
			VolumeName:                pvName,
		},
	}
	if err := c.Create(ctx, target); err != nil {
		return nil, fmt.Errorf("cannot create target PVC %s/%s bound to PersistentVolume %q: %w", targetNamespace, targetName, pvName, err)
	}

	target, err = waitPVCBoundRef(ctx, c, types.NamespacedName{Name: targetName, Namespace: targetNamespace})
	if err != nil {
		return nil, err
	}

	restorePVReclaimPolicy(ctx, c, pvName, originalReclaimPolicy)
	return target, nil
}

// waitPVCBoundRef polls nsn's PVC until it reaches Bound phase, returning its latest state.
func waitPVCBoundRef(ctx context.Context, c client.Client, nsn types.NamespacedName) (*corev1.PersistentVolumeClaim, error) {
	var target *corev1.PersistentVolumeClaim
	err := wait.PollUntilContextTimeout(ctx, PVCBindPollInterval, PVCBindTimeout, true, func(ctx context.Context) (bool, error) {
		var check corev1.PersistentVolumeClaim
		if err := c.Get(ctx, nsn, &check); err != nil {
			if k8serrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		target = &check
		return check.Status.Phase == corev1.ClaimBound, nil
	})
	if err != nil {
		return nil, fmt.Errorf("waiting for PVC %s to bind: %w", nsn.String(), err)
	}
	return target, nil
}

// restorePVReclaimPolicy undoes RebindPVC's temporary switch to Retain, best-effort.
func restorePVReclaimPolicy(ctx context.Context, c client.Client, pvName string, original corev1.PersistentVolumeReclaimPolicy) {
	if original == corev1.PersistentVolumeReclaimRetain {
		return
	}
	var pv corev1.PersistentVolume
	if err := c.Get(ctx, types.NamespacedName{Name: pvName}, &pv); err != nil {
		fmt.Printf("warning: cannot re-fetch PersistentVolume %q to restore its original reclaim policy: %v\n", pvName, err)
		return
	}
	if pv.Spec.PersistentVolumeReclaimPolicy == original {
		return
	}
	pv.Spec.PersistentVolumeReclaimPolicy = original
	if err := c.Update(ctx, &pv); err != nil {
		fmt.Printf("warning: cannot restore PersistentVolume %q reclaim policy to %q: %v\n", pvName, original, err)
	}
}

// WaitPVCBound polls nsn's PVC until it reaches Bound phase.
func WaitPVCBound(ctx context.Context, c client.Client, nsn types.NamespacedName) error {
	_, err := waitPVCBoundRef(ctx, c, nsn)
	return err
}
