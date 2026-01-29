package reconcile

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

// PersistentVolumeClaim reconciles PVC object
// It updates only resource spec
// other fields are ignored
// Makes attempt to resize pvc if needed
// in case of deletion timestamp > 0 does nothing
// user must manually remove finalizer if needed
func PersistentVolumeClaim(ctx context.Context, rclient client.Client, newPVC, prevPVC *corev1.PersistentVolumeClaim) error {
	l := logger.WithContext(ctx)
	currentPVC := &corev1.PersistentVolumeClaim{}
	err := rclient.Get(ctx, types.NamespacedName{Namespace: newPVC.Namespace, Name: newPVC.Name}, currentPVC)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			l.Info(fmt.Sprintf("creating new PVC %s", newPVC.Name))
			if err := rclient.Create(ctx, newPVC); err != nil {
				return fmt.Errorf("cannot create new PVC: %w", err)
			}
			return nil
		}
		return fmt.Errorf("cannot get existing PVC: %w", err)
	}
	if !currentPVC.DeletionTimestamp.IsZero() {
		l.Info("PVC has non zero DeletionTimestamp, skip update." +
			" To fix this, make backup for this pvc, delete pvc finalizers and restore from backup.")
		return nil
	}

	return updatePVC(ctx, rclient, currentPVC, newPVC, prevPVC)
}
