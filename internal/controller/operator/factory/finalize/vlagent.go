package finalize

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	"github.com/VictoriaMetrics/operator/internal/config"
)

// OnVLAgentDelete deletes all vlagent related resources
func OnVLAgentDelete(ctx context.Context, rclient client.Client, cr *vmv1.VLAgent) error {
	if cr.Spec.K8sCollector.Enabled {
		if err := removeFinalizeObjByName(ctx, rclient, &appsv1.DaemonSet{}, cr.PrefixedName(), cr.Namespace); err != nil {
			return err
		}
	} else {
		if err := removeFinalizeObjByName(ctx, rclient, &appsv1.StatefulSet{}, cr.PrefixedName(), cr.Namespace); err != nil {
			return err
		}
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
	if config.IsClusterWideAccessAllowed() {
		if err := removeFinalizeObjByName(ctx, rclient, &rbacv1.ClusterRoleBinding{}, cr.GetRBACName(), cr.GetNamespace()); err != nil {
			return err
		}
		if err := removeFinalizeObjByName(ctx, rclient, &rbacv1.ClusterRole{}, cr.GetRBACName(), cr.GetNamespace()); err != nil {
			return err
		}
		if err := SafeDelete(ctx, rclient, &rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: cr.GetRBACName(), Namespace: cr.GetNamespace()}}); err != nil {
			return err
		}

		if err := SafeDelete(ctx, rclient, &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: cr.GetRBACName(), Namespace: cr.GetNamespace()}}); err != nil {
			return err
		}
	}
	return nil
}
