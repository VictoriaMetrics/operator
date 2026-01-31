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
func PersistentVolumeClaim(ctx context.Context, rclient client.Client, newObj *corev1.PersistentVolumeClaim) error {
	l := logger.WithContext(ctx)
	var existingObj corev1.PersistentVolumeClaim
	nsn := types.NamespacedName{Name: newObj.Name, Namespace: newObj.Namespace}
	if err := rclient.Get(ctx, nsn, &existingObj); err != nil {
		if k8serrors.IsNotFound(err) {
			l.Info(fmt.Sprintf("creating new PVC=%s", nsn))
			if err := rclient.Create(ctx, newObj); err != nil {
				return fmt.Errorf("cannot create new PVC=%s: %w", nsn, err)
			}
			return nil
		}
		return fmt.Errorf("cannot get existing PVC=%s: %w", nsn, err)
	}
	if !existingObj.DeletionTimestamp.IsZero() {
		l.Info(fmt.Sprintf("PVC=%s has non zero DeletionTimestamp, skip update."+
			" To fix this, make backup for this pvc, delete pvc finalizers and restore from backup.", nsn))
		return nil
	}

	return updatePVC(ctx, rclient, &existingObj, newObj)
}
