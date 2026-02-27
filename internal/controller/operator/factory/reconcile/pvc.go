package reconcile

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
func PersistentVolumeClaim(ctx context.Context, rclient client.Client, newObj, prevObj *corev1.PersistentVolumeClaim, owner *metav1.OwnerReference) error {
	l := logger.WithContext(ctx)
	var existingObj corev1.PersistentVolumeClaim
	nsn := types.NamespacedName{Namespace: newObj.Namespace, Name: newObj.Name}
	if err := rclient.Get(ctx, nsn, &existingObj); err != nil {
		if k8serrors.IsNotFound(err) {
			l.Info(fmt.Sprintf("creating new PVC=%s", nsn.String()))
			if err := rclient.Create(ctx, newObj); err != nil {
				return fmt.Errorf("cannot create new PVC=%s: %w", nsn.String(), err)
			}
			return nil
		}
		return fmt.Errorf("cannot get existing PVC=%s: %w", nsn.String(), err)
	}
	if !existingObj.DeletionTimestamp.IsZero() {
		l.Info(fmt.Sprintf("PVC=%s has non zero DeletionTimestamp, skip update."+
			" To fix this, make backup for this pvc, delete pvc finalizers and restore from backup.", nsn.String()))
		return nil
	}

	return updatePVC(ctx, rclient, &existingObj, newObj, prevObj, owner)
}
