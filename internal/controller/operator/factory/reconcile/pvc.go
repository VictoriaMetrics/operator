package reconcile

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
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
		if errors.IsNotFound(err) {
			l.Info("creating new pvc")
			if err := rclient.Create(ctx, newPVC); err != nil {
				return fmt.Errorf("cannot create new pvc: %w", err)
			}
			return nil
		}
		return fmt.Errorf("cannot get existing pvc: %w", err)
	}
	if !currentPVC.DeletionTimestamp.IsZero() {
		l.Info("pvc has non zero DeletionTimestamp, skip update." +
			" To fix this, make backup for this pvc, delete pvc finalizers and restore from backup.")
		return nil
	}
	newSize := newPVC.Spec.Resources.Requests.Storage()
	oldSize := currentPVC.Spec.Resources.Requests.Storage()
	var prevAnnotations map[string]string
	if prevPVC != nil {
		prevAnnotations = prevPVC.Annotations
	}

	isResizeNeeded := mayGrow(ctx, newSize, oldSize)
	if !isResizeNeeded &&
		equality.Semantic.DeepEqual(newPVC.Labels, currentPVC.Labels) &&
		isAnnotationsEqual(currentPVC.Annotations, newPVC.Annotations, prevAnnotations) {
		return nil
	}
	if isResizeNeeded {
		// check if storage class is expandable
		isExpandable, err := isStorageClassExpandable(ctx, rclient, newPVC)
		if err != nil {
			return fmt.Errorf("failed to check storageClass expandability for pvc %s: %v", newPVC.Name, err)
		}
		if !isExpandable {
			// don't return error to caller, since there is no point to requeue and reconcile this when sc is unexpandable
			logger.WithContext(ctx).Info("storage class for PVC doesn't support live resizing", "pvc", newPVC.Name)
			return nil
		}
	}
	logger.WithContext(ctx).Info("updating PersistentVolumeClaim configuration")

	newPVC.Annotations = mergeAnnotations(currentPVC.Annotations, newPVC.Annotations, prevAnnotations)

	newResources := newPVC.Spec.Resources.DeepCopy()
	// keep old spec with new resource requests
	newPVC.Spec = currentPVC.Spec
	newPVC.Spec.Resources = *newResources
	vmv1beta1.AddFinalizer(newPVC, currentPVC)

	if err := rclient.Update(ctx, newPVC); err != nil {
		return err
	}
	return nil
}
