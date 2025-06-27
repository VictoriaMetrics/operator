package finalize

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
)

// OnVMSingleDelete deletes all vmsingle related resources
func OnVMSingleDelete(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMSingle) error {
	// check deployment
	if err := removeFinalizeObjByName(ctx, rclient, &appsv1.Deployment{}, cr.PrefixedName(), cr.Namespace); err != nil {
		return err
	}
	// check service
	if err := removeFinalizeObjByName(ctx, rclient, &corev1.Service{}, cr.PrefixedName(), cr.Namespace); err != nil {
		return err
	}
	if cr.Spec.Storage != nil {
		if err := removeFinalizeObjByNameWithOwnerReference(ctx, rclient, &corev1.PersistentVolumeClaim{}, cr.PrefixedName(), cr.Namespace, cr.Spec.RemovePvcAfterDelete); err != nil {
			return err
		}
	}
	if cr.Spec.ServiceSpec != nil {
		if err := removeFinalizeObjByName(ctx, rclient, &corev1.Service{}, cr.Spec.ServiceSpec.NameOrDefault(cr.PrefixedName()), cr.Namespace); err != nil {
			return err
		}
	}
	if err := removeFinalizeObjByName(ctx, rclient, &corev1.ConfigMap{}, build.ResourceName(build.StreamAggrConfigResourceKind, cr), cr.Namespace); err != nil {
		return err
	}
	if err := deleteSA(ctx, rclient, cr); err != nil {
		return err
	}

	return removeFinalizeObjByName(ctx, rclient, cr, cr.Name, cr.Namespace)
}
