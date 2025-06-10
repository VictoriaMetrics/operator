package finalize

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// OnVMAlertDelete deletes all vmalert related resources
func OnVMAlertDelete(ctx context.Context, rclient client.Client, crd *vmv1beta1.VMAlert) error {
	// check deployment
	if err := removeFinalizeObjByName(ctx, rclient, &appsv1.Deployment{}, crd.PrefixedName(), crd.Namespace); err != nil {
		return err
	}
	// check service
	if err := removeFinalizeObjByName(ctx, rclient, &corev1.Service{}, crd.PrefixedName(), crd.Namespace); err != nil {
		return err
	}
	var cmList corev1.ConfigMapList
	if err := rclient.List(ctx, &cmList, crd.RulesConfigMapSelector()); err != nil {
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
	if err := removeFinalizeObjByName(ctx, rclient, &corev1.Secret{}, crd.TLSAssetName(), crd.Namespace); err != nil {
		return err
	}

	// check PDB
	if crd.Spec.PodDisruptionBudget != nil {
		if err := finalizePBD(ctx, rclient, crd); err != nil {
			return err
		}
	}
	if err := deleteSA(ctx, rclient, crd); err != nil {
		return err
	}

	if crd.Spec.ServiceSpec != nil {
		if err := removeFinalizeObjByName(ctx, rclient, &corev1.Service{}, crd.Spec.ServiceSpec.NameOrDefault(crd.PrefixedName()), crd.Namespace); err != nil {
			return err
		}
	}
	if err := removeFinalizeObjByName(ctx, rclient, crd, crd.Name, crd.Namespace); err != nil {
		return err
	}
	return nil
}
