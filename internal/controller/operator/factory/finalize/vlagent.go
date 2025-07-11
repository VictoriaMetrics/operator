package finalize

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
)

// OnVLAgentDelete deletes all vlagent related resources
func OnVLAgentDelete(ctx context.Context, rclient client.Client, cr *vmv1.VLAgent) error {
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

	// check PDB
	if cr.Spec.PodDisruptionBudget != nil {
		if err := finalizePDB(ctx, rclient, cr); err != nil {
			return err
		}
	}

	// remove from self.
	if err := removeFinalizeObjByName(ctx, rclient, cr, cr.Name, cr.Namespace); err != nil {
		return err
	}
	if err := deleteSA(ctx, rclient, cr); err != nil {
		return err
	}
	return nil
}
