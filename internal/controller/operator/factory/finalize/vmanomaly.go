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
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
)

// OnVMAnomalyDelete deletes all anomaly related resources
func OnVMAnomalyDelete(ctx context.Context, rclient client.Client, cr *vmv1.VMAnomaly) error {
	if err := removeFinalizeObjByName(ctx, rclient, &appsv1.StatefulSet{}, cr.PrefixedName(), cr.Namespace); err != nil {
		return err
	}
	if err := RemoveOrphanedSTSs(ctx, rclient, cr, nil); err != nil {
		return err
	}
	if err := removeFinalizeObjByName(ctx, rclient, &corev1.Secret{}, build.ResourceName(build.SecretConfigResourceKind, cr), cr.Namespace); err != nil {
		return err
	}
	if err := removeFinalizeObjByName(ctx, rclient, &corev1.Secret{}, build.ResourceName(build.TLSAssetsResourceKind, cr), cr.Namespace); err != nil {
		return err
	}
	if cr.Spec.PodDisruptionBudget != nil {
		if err := finalizePDB(ctx, rclient, cr); err != nil {
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
	if err := deleteSA(ctx, rclient, cr); err != nil {
		return err
	}
	return removeFinalizeObjByName(ctx, rclient, cr, cr.Name, cr.Namespace)
}
