package finalize

import (
	"context"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// OnVLSingleDelete deletes all vlogs related resources
func OnVLSingleDelete(ctx context.Context, rclient client.Client, crd *vmv1.VLSingle) error {
	// check deployment
	if err := removeFinalizeObjByName(ctx, rclient, &appsv1.Deployment{}, crd.PrefixedName(), crd.Namespace); err != nil {
		return err
	}
	// check service
	if err := removeFinalizeObjByName(ctx, rclient, &corev1.Service{}, crd.PrefixedName(), crd.Namespace); err != nil {
		return err
	}
	if crd.Spec.Storage != nil {
		if err := removeFinalizeObjByName(ctx, rclient, &corev1.PersistentVolumeClaim{}, crd.PrefixedName(), crd.Namespace); err != nil {
			return err
		}
	}
	if crd.Spec.ServiceSpec != nil {
		if err := removeFinalizeObjByName(ctx, rclient, &corev1.Service{}, crd.Spec.ServiceSpec.NameOrDefault(crd.PrefixedName()), crd.Namespace); err != nil {
			return err
		}
	}
	if err := deleteSA(ctx, rclient, crd); err != nil {
		return err
	}

	return removeFinalizeObjByName(ctx, rclient, crd, crd.Name, crd.Namespace)
}
