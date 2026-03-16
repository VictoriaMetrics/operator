package reconcile

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

// PersistentVolumeClaim reconciles PVC object
// It updates only resource spec
// other fields are ignored
// Makes attempt to resize pvc if needed
// in case of deletion timestamp > 0 does nothing
// user must manually remove finalizer if needed
func PersistentVolumeClaim(ctx context.Context, rclient client.Client, newObj, prevObj *corev1.PersistentVolumeClaim, owner *metav1.OwnerReference) error {
	nsn := types.NamespacedName{Namespace: newObj.Namespace, Name: newObj.Name}
	var existingObj corev1.PersistentVolumeClaim
	err := retryOnConflict(func() error {
		if err := rclient.Get(ctx, nsn, &existingObj); err != nil {
			if k8serrors.IsNotFound(err) {
				logger.WithContext(ctx).Info(fmt.Sprintf("creating new PVC=%s", nsn.String()))
				return rclient.Create(ctx, newObj)
			}
			return fmt.Errorf("cannot get existing PVC=%s: %w", nsn.String(), err)
		}
		if !existingObj.DeletionTimestamp.IsZero() {
			return nil
		}
		return updatePVC(ctx, rclient, &existingObj, newObj, prevObj, owner)
	})
	if err != nil {
		return err
	}
	size := newObj.Spec.Resources.Requests[corev1.ResourceStorage]
	if !existingObj.CreationTimestamp.IsZero() {
		size = existingObj.Spec.Resources.Requests[corev1.ResourceStorage]
	}
	if err = waitForPVCReady(ctx, rclient, nsn, size); err != nil {
		return err
	}
	if !existingObj.DeletionTimestamp.IsZero() {
		logger.WithContext(ctx).Info(fmt.Sprintf("PVC=%s has non zero DeletionTimestamp, skip update."+
			" To fix this, make backup for this pvc, delete pvc finalizers and restore from backup.", nsn.String()))
	}
	return nil
}

func waitForPVCReady(ctx context.Context, rclient client.Client, nsn types.NamespacedName, size resource.Quantity) error {
	var pvc corev1.PersistentVolumeClaim
	return wait.PollUntilContextTimeout(ctx, pvcWaitReadyInterval, pvcWaitReadyTimeout, true, func(ctx context.Context) (done bool, err error) {
		if err := rclient.Get(ctx, nsn, &pvc); err != nil {
			if k8serrors.IsNotFound(err) {
				return false, nil
			}
			return false, fmt.Errorf("cannot get PVC=%s: %w", nsn.String(), err)
		}
		if !pvc.DeletionTimestamp.IsZero() {
			return true, nil
		}
		if len(pvc.Status.Capacity) == 0 {
			return true, nil
		}
		actualSize := pvc.Status.Capacity[corev1.ResourceStorage]
		if actualSize.Cmp(size) < 0 {
			return false, nil
		}
		for _, condition := range pvc.Status.Conditions {
			if condition.Type == corev1.PersistentVolumeClaimResizing && condition.Status == corev1.ConditionTrue {
				return false, nil
			}
		}
		return true, nil
	})
}
