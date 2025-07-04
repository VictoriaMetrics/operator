package finalize

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
)

// OnVLAgentDelete deletes all vlagent related resources
func OnVLAgentDelete(ctx context.Context, rclient client.Client, cr *vmv1.VLAgent) error {
	// check deployment
	if err := removeFinalizeObjByName(ctx, rclient, &appsv1.Deployment{}, cr.PrefixedName(), cr.Namespace); err != nil {
		return err
	}
	if err := removeFinalizeObjByName(ctx, rclient, &appsv1.StatefulSet{}, cr.PrefixedName(), cr.Namespace); err != nil {
		return err
	}
	if err := removeFinalizeObjByName(ctx, rclient, &appsv1.DaemonSet{}, cr.PrefixedName(), cr.Namespace); err != nil {
		return err
	}

	if err := RemoveOrphanedDeployments(ctx, rclient, cr, nil); err != nil {
		return err
	}
	if err := RemoveOrphanedSTSs(ctx, rclient, cr, nil); err != nil {
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
	// config secret
	if err := removeFinalizeObjByName(ctx, rclient, &corev1.Secret{}, cr.PrefixedName(), cr.Namespace); err != nil {
		return err
	}

	// check secret for tls assets
	if err := removeFinalizeObjByName(ctx, rclient, &corev1.Secret{}, build.ResourceName(build.TLSAssetsResourceKind, cr), cr.Namespace); err != nil {
		return err
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
