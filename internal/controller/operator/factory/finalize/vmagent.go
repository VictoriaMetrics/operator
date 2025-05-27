package finalize

import (
	"context"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// OnVMAgentDelete deletes all vmagent related resources
func OnVMAgentDelete(ctx context.Context, rclient client.Client, crd *vmv1beta1.VMAgent) error {
	// check deployment
	if err := removeFinalizeObjByName(ctx, rclient, &appsv1.Deployment{}, crd.PrefixedName(), crd.Namespace); err != nil {
		return err
	}
	if err := removeFinalizeObjByName(ctx, rclient, &appsv1.StatefulSet{}, crd.PrefixedName(), crd.Namespace); err != nil {
		return err
	}
	if err := removeFinalizeObjByName(ctx, rclient, &appsv1.DaemonSet{}, crd.PrefixedName(), crd.Namespace); err != nil {
		return err
	}

	if err := RemoveOrphanedDeployments(ctx, rclient, crd, nil); err != nil {
		return err
	}
	if err := RemoveOrphanedStatefulSets(ctx, rclient, crd, nil); err != nil {
		return err
	}
	// check service
	if err := removeFinalizeObjByName(ctx, rclient, &corev1.Service{}, crd.PrefixedName(), crd.Namespace); err != nil {
		return err
	}
	if crd.Spec.ServiceSpec != nil {
		if err := removeFinalizeObjByName(ctx, rclient, &corev1.Service{}, crd.Spec.ServiceSpec.NameOrDefault(crd.PrefixedName()), crd.Namespace); err != nil {
			return err
		}
	}
	// config secret
	if err := removeFinalizeObjByName(ctx, rclient, &corev1.Secret{}, crd.PrefixedName(), crd.Namespace); err != nil {
		return err
	}

	// check secret for tls assests
	if err := removeFinalizeObjByName(ctx, rclient, &corev1.Secret{}, crd.TLSAssetName(), crd.Namespace); err != nil {
		return err
	}

	// check relabelAsset
	if err := removeFinalizeObjByName(ctx, rclient, &corev1.ConfigMap{}, crd.RelabelingAssetName(), crd.Namespace); err != nil {
		return err
	}
	if err := removeFinalizeObjByName(ctx, rclient, &corev1.ConfigMap{}, crd.StreamAggrConfigName(), crd.Namespace); err != nil {
		return err
	}

	// check PDB
	if crd.Spec.PodDisruptionBudget != nil {
		if err := finalizePBD(ctx, rclient, crd); err != nil {
			return err
		}
	}
	// remove vmagents service discovery rbac.
	if config.IsClusterWideAccessAllowed() {
		if err := removeFinalizeObjByName(ctx, rclient, &rbacv1.ClusterRoleBinding{}, crd.GetClusterRoleName(), crd.GetNamespace()); err != nil {
			return err
		}
		if err := removeFinalizeObjByName(ctx, rclient, &rbacv1.ClusterRole{}, crd.GetClusterRoleName(), crd.GetNamespace()); err != nil {
			return err
		}
		if err := SafeDelete(ctx, rclient, &rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: crd.GetClusterRoleName(), Namespace: crd.GetNamespace()}}); err != nil {
			return err
		}

		if err := SafeDelete(ctx, rclient, &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: crd.GetClusterRoleName(), Namespace: crd.GetNamespace()}}); err != nil {
			return err
		}
	} else {
		if err := removeFinalizeObjByName(ctx, rclient, &rbacv1.RoleBinding{}, crd.GetClusterRoleName(), crd.GetNamespace()); err != nil {
			return err
		}
		if err := removeFinalizeObjByName(ctx, rclient, &rbacv1.Role{}, crd.GetClusterRoleName(), crd.GetNamespace()); err != nil {
			return err
		}
		if err := SafeDelete(ctx, rclient, &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: crd.GetClusterRoleName(), Namespace: crd.GetNamespace()}}); err != nil {
			return err
		}

		if err := SafeDelete(ctx, rclient, &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: crd.GetClusterRoleName(), Namespace: crd.GetNamespace()}}); err != nil {
			return err
		}
	}

	if crd.Spec.AdditionalScrapeConfigs != nil {
		if err := removeFinalizeObjByName(ctx, rclient, &corev1.Secret{}, crd.Spec.AdditionalScrapeConfigs.Name, crd.Namespace); err != nil {
			return err
		}
	}
	for _, rw := range crd.Spec.RemoteWrite {
		if rw.UrlRelabelConfig != nil {
			if err := removeFinalizeObjByName(ctx, rclient, &corev1.ConfigMap{}, rw.UrlRelabelConfig.Name, crd.Namespace); err != nil {
				return err
			}
		}
	}
	// remove from self.
	if err := removeFinalizeObjByName(ctx, rclient, crd, crd.Name, crd.Namespace); err != nil {
		return err
	}
	if err := deleteSA(ctx, rclient, crd); err != nil {
		return err
	}
	return nil
}
