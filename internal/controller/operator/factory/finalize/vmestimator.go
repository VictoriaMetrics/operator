package finalize

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
)

// OnVMEstimatorDelete removes all objects related to VMEstimator
func OnVMEstimatorDelete(ctx context.Context, rclient client.Client, cr *vmv1.VMEstimator) error {
	if err := OnVMEstimatorSingleDelete(ctx, rclient, cr, false); err != nil {
		return fmt.Errorf("cannot remove single component objects: %w", err)
	}
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
			Name:      cr.PrefixedName(vmv1beta1.ClusterComponentRoot),
			Namespace: ns,
		}},
		cr,
	}
	deleteOwnerReferences := make([]bool, len(objsToRemove))
	return removeFinalizers(ctx, rclient, objsToRemove, deleteOwnerReferences, b)
}

// OnVMEstimatorSingleDelete removes all objects related to single-node vmestimator component
func OnVMEstimatorSingleDelete(ctx context.Context, rclient client.Client, cr *vmv1.VMEstimator, shouldRemove bool) error {
	b := build.NewChildBuilder(cr, vmv1.VMEstimatorComponentSingle)
	if err := RemoveOrphanedVMServiceScrapes(ctx, rclient, b, nil, shouldRemove); err != nil {
		return fmt.Errorf("cannot remove orphaned serviceScrapes: %w", err)
	}
	objMeta := metav1.ObjectMeta{
		Namespace: cr.GetNamespace(),
		Name:      cr.PrefixedName(vmv1.VMEstimatorComponentSingle),
	}
	objsToRemove := []client.Object{
		&appsv1.Deployment{ObjectMeta: objMeta},
		&policyv1.PodDisruptionBudget{ObjectMeta: objMeta},
	}
	if config.MustGetBaseConfig().VPAAPIEnabled {
		objsToRemove = append(objsToRemove, &vpav1.VerticalPodAutoscaler{ObjectMeta: objMeta})
	}
	if shouldRemove {
		return SafeDeleteWithFinalizer(ctx, rclient, objsToRemove, b)
	}
	deleteOwnerReferences := make([]bool, len(objsToRemove))
	return removeFinalizers(ctx, rclient, objsToRemove, deleteOwnerReferences, b)
}
