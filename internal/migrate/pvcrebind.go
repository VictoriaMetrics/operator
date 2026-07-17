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

	ReclaimPolicyRestorePollInterval = 2 * time.Second
	ReclaimPolicyRestoreTimeout      = 30 * time.Second

	ReclaimPolicyPropagationPollInterval = 100 * time.Millisecond
	ReclaimPolicyPropagationTimeout      = 2 * time.Second
)

const originalReclaimPolicyAnnotation = "migrate.victoriametrics.com/original-reclaim-policy"
const reboundFromAnnotation = "migrate.victoriametrics.com/rebound-from"

func rebindPVC(ctx context.Context, c client.Client, sourcePVC *corev1.PersistentVolumeClaim, targetName, targetNamespace string, pvcLabels map[string]string) (_ *corev1.PersistentVolumeClaim, err error) {
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

	var existing corev1.PersistentVolumeClaim
	getErr := c.Get(ctx, types.NamespacedName{Name: targetName, Namespace: targetNamespace}, &existing)
	switch {
	case getErr == nil:
		if existing.Spec.VolumeName != pvName {
			return nil, fmt.Errorf("target PVC %s/%s already exists but is bound to PersistentVolume %q, not %q — refusing to treat it as this rebind's target",
				targetNamespace, targetName, existing.Spec.VolumeName, pvName)
		}
		if existing.DeletionTimestamp != nil {
			return nil, fmt.Errorf("target PVC %s/%s already exists but is terminating — wait for it to finish deleting, or delete it manually, before retrying", targetNamespace, targetName)
		}
		wantReboundFrom := sourcePVC.Namespace + "/" + sourcePVC.Name
		if existing.Status.Phase != corev1.ClaimBound {
			ref := pv.Spec.ClaimRef
			needsResume := ref == nil || ref.Namespace != targetNamespace || ref.Name != targetName
			if needsResume && existing.Annotations[reboundFromAnnotation] != wantReboundFrom {
				return nil, fmt.Errorf("target PVC %s/%s already exists and is marked as rebound from %q, not %q — refusing to delete or repoint the source PVC on its behalf",
					targetNamespace, targetName, existing.Annotations[reboundFromAnnotation], wantReboundFrom)
			}
			if needsResume {
				if err := deleteSourceAndRepointClaimRef(ctx, c, sourcePVC, &pv, targetNamespace, targetName); err != nil {
					return nil, err
				}
			}
			bound, err := waitPVCBoundRef(ctx, c, types.NamespacedName{Name: targetName, Namespace: targetNamespace})
			if err != nil {
				return nil, err
			}
			existing = *bound
		}
		if existing.Annotations[reboundFromAnnotation] != "" && existing.Annotations[reboundFromAnnotation] != wantReboundFrom {
			return nil, fmt.Errorf("target PVC %s/%s already exists and is marked as rebound from %q, not %q — refusing to treat it as this rebind's target",
				targetNamespace, targetName, existing.Annotations[reboundFromAnnotation], wantReboundFrom)
		}
		needsUpdate := existing.Annotations[reboundFromAnnotation] == ""
		if needsUpdate {
			if ref := pv.Spec.ClaimRef; ref == nil || ref.Namespace != targetNamespace || ref.Name != targetName {
				return nil, fmt.Errorf("target PVC %s/%s reports Bound to PersistentVolume %q, but that PV's claimRef doesn't point back to it — refusing to adopt it as this rebind's target",
					targetNamespace, targetName, pvName)
			}
		}
		if needsUpdate {
			if existing.Annotations == nil {
				existing.Annotations = map[string]string{}
			}
			existing.Annotations[reboundFromAnnotation] = wantReboundFrom
		}
		if missingLabels(existing.Labels, pvcLabels) {
			existing.Labels = mergeLabels(existing.Labels, pvcLabels)
			needsUpdate = true
		}
		if needsUpdate {
			if err := c.Update(ctx, &existing); err != nil {
				return nil, fmt.Errorf("cannot mark target PVC %s/%s as rebound: %w", targetNamespace, targetName, err)
			}
		}
		if _, pending := pv.Annotations[originalReclaimPolicyAnnotation]; !pending {
			return &existing, nil
		}
		originalReclaimPolicy, err := captureOriginalReclaimPolicy(ctx, c, &pv)
		if err != nil {
			return nil, err
		}
		return &existing, restorePVReclaimPolicy(ctx, c, pvName, originalReclaimPolicy)
	case !k8serrors.IsNotFound(getErr):
		return nil, fmt.Errorf("cannot check for existing target PVC %s/%s: %w", targetNamespace, targetName, getErr)
	}

	target := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:        targetName,
			Namespace:   targetNamespace,
			Labels:      pvcLabels,
			Annotations: map[string]string{reboundFromAnnotation: sourcePVC.Namespace + "/" + sourcePVC.Name},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: sourcePVC.Spec.AccessModes,
			Resources:   sourcePVC.Spec.Resources,
			StorageClassName: func() *string {
				if sourcePVC.Spec.StorageClassName != nil {
					return sourcePVC.Spec.StorageClassName
				}
				return &pv.Spec.StorageClassName
			}(),
			VolumeMode:                sourcePVC.Spec.VolumeMode,
			VolumeAttributesClassName: sourcePVC.Spec.VolumeAttributesClassName,
			VolumeName:                pvName,
		},
	}

	if ref := pv.Spec.ClaimRef; ref != nil && ref.Namespace == targetNamespace && ref.Name == targetName {
		originalReclaimPolicy, err := captureOriginalReclaimPolicy(ctx, c, &pv)
		if err != nil {
			return nil, err
		}
		if err := createTargetPVC(ctx, c, target, pvName, targetNamespace, targetName); err != nil {
			return nil, err
		}
		return finishRebindPVC(ctx, c, pvName, targetNamespace, targetName, originalReclaimPolicy)
	}

	if err := verifyClaimRefIdentifies(&pv, sourcePVC); err != nil {
		return nil, err
	}
	if err := dryRunCreate(ctx, c, target); err != nil {
		return nil, fmt.Errorf("target PVC %s/%s would be rejected by the API server: %w", targetNamespace, targetName, err)
	}

	originalReclaimPolicy, err := captureOriginalReclaimPolicy(ctx, c, &pv)
	if err != nil {
		return nil, err
	}
	if err := waitReclaimPolicyPropagated(ctx, c, pvName); err != nil {
		return nil, err
	}

	if err := createTargetPVC(ctx, c, target, pvName, targetNamespace, targetName); err != nil {
		return nil, err
	}

	if err := deleteSourceAndRepointClaimRef(ctx, c, sourcePVC, &pv, targetNamespace, targetName); err != nil {
		return nil, err
	}

	return finishRebindPVC(ctx, c, pvName, targetNamespace, targetName, originalReclaimPolicy)
}

func deleteSourceAndRepointClaimRef(ctx context.Context, c client.Client, sourcePVC *corev1.PersistentVolumeClaim, pv *corev1.PersistentVolume, targetNamespace, targetName string) error {
	if sourcePVC.Name != targetName || sourcePVC.Namespace != targetNamespace {
		if err := c.Delete(ctx, sourcePVC, client.Preconditions{UID: &sourcePVC.UID}); err != nil && !k8serrors.IsNotFound(err) {
			return fmt.Errorf("cannot delete source PVC %s/%s: %w", sourcePVC.Namespace, sourcePVC.Name, err)
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
			return fmt.Errorf("waiting for source PVC %s/%s to be deleted: %w", sourcePVC.Namespace, sourcePVC.Name, err)
		}
	}

	if err := c.Get(ctx, types.NamespacedName{Name: pv.Name}, pv); err != nil {
		return fmt.Errorf("cannot re-fetch PersistentVolume %q: %w", pv.Name, err)
	}
	if ref := pv.Spec.ClaimRef; ref != nil && ref.Namespace == targetNamespace && ref.Name == targetName {
		return nil
	}
	if err := verifyClaimRefIdentifies(pv, sourcePVC); err != nil {
		return err
	}
	pv.Spec.ClaimRef = &corev1.ObjectReference{
		Kind:      "PersistentVolumeClaim",
		Namespace: targetNamespace,
		Name:      targetName,
	}
	if err := c.Update(ctx, pv); err != nil {
		return fmt.Errorf("cannot repoint claimRef on PersistentVolume %q to target PVC %s/%s: %w", pv.Name, targetNamespace, targetName, err)
	}
	return nil
}

func createTargetPVC(ctx context.Context, c client.Client, target *corev1.PersistentVolumeClaim, pvName, targetNamespace, targetName string) error {
	if err := c.Create(ctx, target); err != nil {
		if !k8serrors.IsAlreadyExists(err) {
			return fmt.Errorf("cannot create target PVC %s/%s bound to PersistentVolume %q: %w", targetNamespace, targetName, pvName, err)
		}
		var existing corev1.PersistentVolumeClaim
		if err := c.Get(ctx, types.NamespacedName{Name: targetName, Namespace: targetNamespace}, &existing); err != nil {
			return fmt.Errorf("cannot fetch existing target PVC %s/%s: %w", targetNamespace, targetName, err)
		}
		if existing.DeletionTimestamp != nil || existing.Spec.VolumeName != pvName || existing.Annotations[reboundFromAnnotation] != target.Annotations[reboundFromAnnotation] {
			return fmt.Errorf("target PVC %s/%s appeared during rebind but isn't the expected PVC for PersistentVolume %q — refusing to adopt what may be an unrelated, coincidentally-named PVC", targetNamespace, targetName, pvName)
		}
	}
	return nil
}

func missingLabels(have, want map[string]string) bool {
	for k, v := range want {
		if have[k] != v {
			return true
		}
	}
	return false
}

func mergeLabels(have, want map[string]string) map[string]string {
	merged := make(map[string]string, len(have)+len(want))
	for k, v := range have {
		merged[k] = v
	}
	for k, v := range want {
		merged[k] = v
	}
	return merged
}

func finishRebindPVC(ctx context.Context, c client.Client, pvName, targetNamespace, targetName string, originalReclaimPolicy corev1.PersistentVolumeReclaimPolicy) (*corev1.PersistentVolumeClaim, error) {
	target, err := waitPVCBoundRef(ctx, c, types.NamespacedName{Name: targetName, Namespace: targetNamespace})
	if err != nil {
		return nil, err
	}
	return target, restorePVReclaimPolicy(ctx, c, pvName, originalReclaimPolicy)
}

func waitReclaimPolicyPropagated(ctx context.Context, c client.Client, pvName string) error {
	err := wait.PollUntilContextTimeout(ctx, ReclaimPolicyPropagationPollInterval, ReclaimPolicyPropagationTimeout, true, func(ctx context.Context) (bool, error) {
		var check corev1.PersistentVolume
		if err := c.Get(ctx, types.NamespacedName{Name: pvName}, &check); err != nil {
			return false, err
		}
		return check.Spec.PersistentVolumeReclaimPolicy == corev1.PersistentVolumeReclaimRetain, nil
	})
	if err != nil {
		return fmt.Errorf("waiting for PersistentVolume %q reclaim policy to propagate to Retain: %w", pvName, err)
	}
	return nil
}

func verifyClaimRefIdentifies(pv *corev1.PersistentVolume, pvc *corev1.PersistentVolumeClaim) error {
	ref := pv.Spec.ClaimRef
	if ref == nil {
		return fmt.Errorf("PersistentVolume %q has no claimRef — refusing to treat it as bound to source PVC %s/%s and risk acquiring a volume another claim is targeting",
			pv.Name, pvc.Namespace, pvc.Name)
	}
	if ref.Namespace != pvc.Namespace || ref.Name != pvc.Name || (ref.UID != "" && pvc.UID != "" && ref.UID != pvc.UID) {
		return fmt.Errorf("PersistentVolume %q's claimRef no longer identifies source PVC %s/%s (now %s/%s) — refusing to touch it and risk affecting an unrelated claim",
			pv.Name, pvc.Namespace, pvc.Name, ref.Namespace, ref.Name)
	}
	return nil
}

func captureOriginalReclaimPolicy(ctx context.Context, c client.Client, pv *corev1.PersistentVolume) (corev1.PersistentVolumeReclaimPolicy, error) {
	original, hadAnnotation := pv.Spec.PersistentVolumeReclaimPolicy, false
	if saved, ok := pv.Annotations[originalReclaimPolicyAnnotation]; ok {
		original, hadAnnotation = corev1.PersistentVolumeReclaimPolicy(saved), true
	}
	if pv.Spec.PersistentVolumeReclaimPolicy == corev1.PersistentVolumeReclaimRetain && (hadAnnotation || original == corev1.PersistentVolumeReclaimRetain) {
		return original, nil
	}
	var captured corev1.PersistentVolumeReclaimPolicy
	err := wait.PollUntilContextTimeout(ctx, ReclaimPolicyRestorePollInterval, ReclaimPolicyRestoreTimeout, true, func(ctx context.Context) (bool, error) {
		if pv.Annotations == nil {
			pv.Annotations = map[string]string{}
		}
		pv.Annotations[originalReclaimPolicyAnnotation] = string(original)
		pv.Spec.PersistentVolumeReclaimPolicy = corev1.PersistentVolumeReclaimRetain
		if err := c.Update(ctx, pv); err != nil {
			if k8serrors.IsConflict(err) {
				return false, c.Get(ctx, types.NamespacedName{Name: pv.Name}, pv)
			}
			return false, err
		}
		captured = original
		return true, nil
	})
	if err != nil {
		return "", fmt.Errorf("cannot patch PersistentVolume %q to Retain reclaim policy: %w", pv.Name, err)
	}
	return captured, nil
}

func ensureReclaimPolicyRestored(ctx context.Context, c client.Client, nsn types.NamespacedName) error {
	var pvc corev1.PersistentVolumeClaim
	if err := c.Get(ctx, nsn, &pvc); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("cannot check PVC %s for a pending reclaim-policy restore: %w", nsn, err)
	}
	if pvc.Spec.VolumeName == "" {
		return nil
	}
	var pv corev1.PersistentVolume
	if err := c.Get(ctx, types.NamespacedName{Name: pvc.Spec.VolumeName}, &pv); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("cannot check PersistentVolume %q for a pending reclaim-policy restore: %w", pvc.Spec.VolumeName, err)
	}
	original, ok := pv.Annotations[originalReclaimPolicyAnnotation]
	if !ok {
		return nil
	}
	if pvc.Status.Phase != corev1.ClaimBound {
		if _, err := waitPVCBoundRef(ctx, c, nsn); err != nil {
			return err
		}
	}
	return restorePVReclaimPolicy(ctx, c, pv.Name, corev1.PersistentVolumeReclaimPolicy(original))
}

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

func restorePVReclaimPolicy(ctx context.Context, c client.Client, pvName string, original corev1.PersistentVolumeReclaimPolicy) error {
	err := wait.PollUntilContextTimeout(ctx, ReclaimPolicyRestorePollInterval, ReclaimPolicyRestoreTimeout, true, func(ctx context.Context) (bool, error) {
		var pv corev1.PersistentVolume
		if err := c.Get(ctx, types.NamespacedName{Name: pvName}, &pv); err != nil {
			return false, err
		}
		_, hasAnnotation := pv.Annotations[originalReclaimPolicyAnnotation]
		if pv.Spec.PersistentVolumeReclaimPolicy == original && !hasAnnotation {
			return true, nil
		}
		pv.Spec.PersistentVolumeReclaimPolicy = original
		delete(pv.Annotations, originalReclaimPolicyAnnotation)
		if err := c.Update(ctx, &pv); err != nil {
			if k8serrors.IsConflict(err) {
				return false, nil
			}
			return false, err
		}
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("cannot restore PersistentVolume %q reclaim policy to %q: %w", pvName, original, err)
	}
	return nil
}
