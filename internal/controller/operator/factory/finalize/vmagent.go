package finalize

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
)

// OnVMAgentDelete deletes all vmagent related resources
func OnVMAgentDelete(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAgent) error {
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

	// check secret for tls assests
	if err := removeFinalizeObjByName(ctx, rclient, &corev1.Secret{}, build.ResourceName(build.TLSAssetsResourceKind, cr), cr.Namespace); err != nil {
		return err
	}

	// check relabelAsset
	if err := removeFinalizeObjByName(ctx, rclient, &corev1.ConfigMap{}, cr.RelabelingAssetName(), cr.Namespace); err != nil {
		return err
	}
	if err := removeFinalizeObjByName(ctx, rclient, &corev1.ConfigMap{}, cr.StreamAggrConfigName(), cr.Namespace); err != nil {
		return err
	}

	// check PDB
	if cr.Spec.PodDisruptionBudget != nil {
		if err := finalizePBD(ctx, rclient, cr); err != nil {
			return err
		}
	}
	// remove vmagents service discovery rbac.
	if config.IsClusterWideAccessAllowed() {
		if err := removeFinalizeObjByName(ctx, rclient, &rbacv1.ClusterRoleBinding{}, cr.GetClusterRoleName(), cr.GetNamespace()); err != nil {
			return err
		}
		if err := removeFinalizeObjByName(ctx, rclient, &rbacv1.ClusterRole{}, cr.GetClusterRoleName(), cr.GetNamespace()); err != nil {
			return err
		}
		if err := SafeDelete(ctx, rclient, &rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: cr.GetClusterRoleName(), Namespace: cr.GetNamespace()}}); err != nil {
			return err
		}

		if err := SafeDelete(ctx, rclient, &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: cr.GetClusterRoleName(), Namespace: cr.GetNamespace()}}); err != nil {
			return err
		}
	} else {
		if err := removeFinalizeObjByName(ctx, rclient, &rbacv1.RoleBinding{}, cr.GetClusterRoleName(), cr.GetNamespace()); err != nil {
			return err
		}
		if err := removeFinalizeObjByName(ctx, rclient, &rbacv1.Role{}, cr.GetClusterRoleName(), cr.GetNamespace()); err != nil {
			return err
		}
		if err := SafeDelete(ctx, rclient, &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: cr.GetClusterRoleName(), Namespace: cr.GetNamespace()}}); err != nil {
			return err
		}

		if err := SafeDelete(ctx, rclient, &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: cr.GetClusterRoleName(), Namespace: cr.GetNamespace()}}); err != nil {
			return err
		}
	}

	if cr.Spec.AdditionalScrapeConfigs != nil {
		if err := removeFinalizeObjByName(ctx, rclient, &corev1.Secret{}, cr.Spec.AdditionalScrapeConfigs.Name, cr.Namespace); err != nil {
			return err
		}
	}
	for _, rw := range cr.Spec.RemoteWrite {
		if rw.UrlRelabelConfig != nil {
			if err := removeFinalizeObjByName(ctx, rclient, &corev1.ConfigMap{}, rw.UrlRelabelConfig.Name, cr.Namespace); err != nil {
				return err
			}
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
