package finalize

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// OnVMAlertManagerDelete deletes all alertmanager related resources
func OnVMAlertManagerDelete(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAlertmanager) error {
	// check deployment
	if err := removeFinalizeObjByName(ctx, rclient, &appsv1.StatefulSet{}, cr.PrefixedName(), cr.Namespace); err != nil {
		return err
	}
	// check service
	if err := removeFinalizeObjByName(ctx, rclient, &corev1.Service{}, cr.PrefixedName(), cr.Namespace); err != nil {
		return err
	}
	if cr.Spec.ServiceSpec != nil {
		if err := removeFinalizeObjByName(ctx, rclient, &corev1.Service{}, cr.Spec.ServiceSpec.NameOrDefault(cr.PrefixedName()), cr.Namespace); err != nil {
			return err
		}
	}

	// check config secret finalizer.
	if err := removeFinalizeObjByName(ctx, rclient, &corev1.Secret{}, cr.ConfigSecretName(), cr.Namespace); err != nil {
		return err
	}
	if len(cr.Spec.ConfigSecret) > 0 {
		// execute it for backward-compatibility
		if err := removeFinalizeObjByName(ctx, rclient, &corev1.Secret{}, cr.Spec.ConfigSecret, cr.Namespace); err != nil {
			return err
		}
	}

	// check PDB
	if cr.Spec.PodDisruptionBudget != nil {
		if err := finalizePDB(ctx, rclient, cr); err != nil {
			return err
		}
	}

	if err := deleteSA(ctx, rclient, cr); err != nil {
		return err
	}
	if err := removeConfigReloaderRole(ctx, rclient, cr); err != nil {
		return err
	}

	return removeFinalizeObjByName(ctx, rclient, cr, cr.Name, cr.Namespace)
}
