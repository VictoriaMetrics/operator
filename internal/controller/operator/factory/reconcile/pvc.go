package reconcile

import (
	"context"
	"fmt"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PersistentVolumeClaim reconciles PVC object
// It updates only resource spec
// other fields are ignored
// Makes attempt to resize pvc if needed
// in case of deletion timestamp > 0 does nothing
// user must manually remove finalizer if needed
func PersistentVolumeClaim(ctx context.Context, rclient client.Client, pvc *corev1.PersistentVolumeClaim) error {
	l := logger.WithContext(ctx)
	existPvc := &corev1.PersistentVolumeClaim{}
	err := rclient.Get(ctx, types.NamespacedName{Namespace: pvc.Namespace, Name: pvc.Name}, existPvc)
	if err != nil {
		if errors.IsNotFound(err) {
			l.Info("creating new pvc")
			if err := rclient.Create(ctx, pvc); err != nil {
				return fmt.Errorf("cannot create new pvc: %w", err)
			}
			return nil
		}
		return fmt.Errorf("cannot get existing pvc: %w", err)
	}
	if !existPvc.DeletionTimestamp.IsZero() {
		l.Info("pvc has non zero DeletionTimestamp, skip update. To fix this, make backup for this pvc, delete pvc finalizers and restore from backup.")
		return nil
	}
	newSize := pvc.Spec.Resources.Requests.Storage()
	oldSize := pvc.Spec.Resources.Requests.Storage()

	pvc.Annotations = labels.Merge(existPvc.Annotations, pvc.Annotations)
	isResizeNeeded := mayGrow(ctx, newSize, oldSize)
	if !isResizeNeeded &&
		equality.Semantic.DeepEqual(pvc.Labels, existPvc.Labels) &&
		equality.Semantic.DeepEqual(pvc.Annotations, existPvc.Annotations) {
		return nil
	}
	if isResizeNeeded {
		// check if storage class is expandable
		isExpandable, err := isStorageClassExpandable(ctx, rclient, pvc)
		if err != nil {
			return fmt.Errorf("failed to check storageClass expandability for pvc %s: %v", pvc.Name, err)
		}
		if !isExpandable {
			// don't return error to caller, since there is no point to requeue and reconcile this when sc is unexpandable
			logger.WithContext(ctx).Info("storage class for PVC doesn't support live resizing", "pvc", pvc.Name)
			return nil
		}
	}
	logger.WithContext(ctx).Info("updating PersistentVolumeClaim configuration")
	newResources := pvc.Spec.Resources.DeepCopy()
	// keep old spec with new resource requests
	pvc.Spec = existPvc.Spec
	pvc.Spec.Resources = *newResources
	vmv1beta1.AddFinalizer(pvc, existPvc)

	if err := rclient.Update(ctx, pvc); err != nil {
		return err
	}
	return nil
}
