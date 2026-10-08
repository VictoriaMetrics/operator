package finalize

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
)

// OnVMEstimatorDelete removes all objects related to VMEstimator
func OnVMEstimatorDelete(ctx context.Context, rclient client.Client, cr *vmv1.VMEstimator) error {
	if err := OnSelectDelete(ctx, rclient, cr, false); err != nil {
		return fmt.Errorf("cannot remove select component objects: %w", err)
	}
	if err := OnStorageDelete(ctx, rclient, cr, false); err != nil {
		return fmt.Errorf("cannot remove storage component objects: %w", err)
	}
	b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentCommon)
	if err := RemoveOrphanedServices(ctx, rclient, b, nil, false); err != nil {
		return fmt.Errorf("cannot remove orphaned services: %w", err)
	}
	ns := cr.GetNamespace()
	objsToRemove := []client.Object{
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{
			Name:      cr.GetServiceAccountName(),
			Namespace: ns,
		}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
			Name:      cr.GetConfigMapName(),
			Namespace: ns,
		}},
		cr,
	}
	deleteOwnerReferences := make([]bool, len(objsToRemove))
	return removeFinalizers(ctx, rclient, objsToRemove, deleteOwnerReferences, b)
}
