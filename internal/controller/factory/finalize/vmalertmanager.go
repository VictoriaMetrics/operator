package finalize

import (
	"context"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// OnVMAlertManagerDelete deletes all alertmanager related resources
func OnVMAlertManagerDelete(ctx context.Context, rclient client.Client, crd *vmv1beta1.VMAlertmanager) error {
	// check deployment
	if err := removeFinalizeObjByName(ctx, rclient, &appsv1.StatefulSet{}, crd.PrefixedName(), crd.Namespace); err != nil {
		return err
	}
	// check service
	if err := removeFinalizeObjByName(ctx, rclient, &v1.Service{}, crd.PrefixedName(), crd.Namespace); err != nil {
		return err
	}
	if crd.Spec.ServiceSpec != nil {
		if err := removeFinalizeObjByName(ctx, rclient, &v1.Service{}, crd.Spec.ServiceSpec.NameOrDefault(crd.PrefixedName()), crd.Namespace); err != nil {
			return err
		}
	}

	// check config secret finalizer.
	if err := removeFinalizeObjByName(ctx, rclient, &v1.Secret{}, crd.ConfigSecretName(), crd.Namespace); err != nil {
		return err
	}
	if len(crd.Spec.ConfigSecret) > 0 {
		// execute it for backward-compatibility
		if err := removeFinalizeObjByName(ctx, rclient, &v1.Secret{}, crd.Spec.ConfigSecret, crd.Namespace); err != nil {
			return err
		}
	}

	// check PDB
	if err := finalizePBD(ctx, rclient, crd); err != nil {
		return err
	}

	if err := deleteSA(ctx, rclient, crd); err != nil {
		return err
	}
	if err := removeConfigReloaderRole(ctx, rclient, crd); err != nil {
		return err
	}

	return removeFinalizeObjByName(ctx, rclient, crd, crd.Name, crd.Namespace)
}
