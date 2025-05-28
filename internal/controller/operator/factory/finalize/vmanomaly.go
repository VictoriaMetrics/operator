package finalize

import (
	"context"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// OnVMAnomalyDelete deletes all anomaly related resources
func OnVMAnomalyDelete(ctx context.Context, rclient client.Client, crd *vmv1.VMAnomaly) error {
	// check deployment
	if err := removeFinalizeObjByName(ctx, rclient, &appsv1.Deployment{}, crd.PrefixedName(), crd.Namespace); err != nil {
		return err
	}
	if err := removeFinalizeObjByName(ctx, rclient, &appsv1.StatefulSet{}, crd.PrefixedName(), crd.Namespace); err != nil {
		return err
	}

	if err := RemoveOrphanedDeployments(ctx, rclient, crd, nil); err != nil {
		return err
	}
	if err := RemoveOrphanedStatefulSets(ctx, rclient, crd, nil); err != nil {
		return err
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
	if crd.Spec.PodDisruptionBudget != nil {
		if err := finalizePBD(ctx, rclient, crd); err != nil {
			return err
		}
	}

	if err := deleteSA(ctx, rclient, crd); err != nil {
		return err
	}
	if err := removeConfigReloaderRole(ctx, rclient, crd); err != nil {
		return err
	}

	return removeFinalizeObjByName(ctx, rclient, crd, crd.Name, crd.Namespace)
}
