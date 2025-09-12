package finalize

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
)

// OnVMAlertDelete deletes all vmalert related resources
func OnVMAlertDelete(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAlert) error {
	// check deployment
	if err := removeFinalizeObjByName(ctx, rclient, &appsv1.Deployment{}, cr.PrefixedName(), cr.Namespace); err != nil {
		return err
	}
	// check service
	if err := removeFinalizeObjByName(ctx, rclient, &corev1.Service{}, cr.PrefixedName(), cr.Namespace); err != nil {
		return err
	}
	if err := removeFinalizeObjByName(ctx, rclient, &corev1.Secret{}, build.ResourceName(build.SecretConfigResourceKind, cr), cr.Namespace); err != nil {
		return err
	}
	var cmList corev1.ConfigMapList
	if err := rclient.List(ctx, &cmList, cr.RulesConfigMapSelector()); err != nil {
		return err
	}
	for _, cm := range cmList.Items {
		if err := vmv1beta1.RemoveFinalizer(&cm, func(o client.Object) error {
			return patchReplaceFinalizers(ctx, rclient, o)
		}); err != nil {
			return fmt.Errorf("failed to remove finalizer from vmalert cm=%q: %w", cm.Name, err)
		}
	}
	// check secret
	if err := removeFinalizeObjByName(ctx, rclient, &corev1.Secret{}, build.ResourceName(build.TLSAssetsResourceKind, cr), cr.Namespace); err != nil {
		return err
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

	if cr.Spec.ServiceSpec != nil {
		if err := removeFinalizeObjByName(ctx, rclient, &corev1.Service{}, cr.Spec.ServiceSpec.NameOrDefault(cr.PrefixedName()), cr.Namespace); err != nil {
			return err
		}
	}
	if err := removeFinalizeObjByName(ctx, rclient, cr, cr.Name, cr.Namespace); err != nil {
		return err
	}
	return nil
}
