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

// OnVMSingleDelete deletes all vmsingle related resources
func OnVMSingleDelete(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMSingle) error {
	ns := cr.GetNamespace()
	if config.IsClusterWideAccessAllowed() {
		objMeta := metav1.ObjectMeta{
			Name: cr.GetRBACName(),
		}
		objsToRemove := []client.Object{
			&rbacv1.ClusterRoleBinding{ObjectMeta: objMeta},
			&rbacv1.ClusterRole{ObjectMeta: objMeta},
		}
		if err := SafeDeleteWithFinalizer(ctx, rclient, objsToRemove, cr); err != nil {
			return err
		}
	}
	objMeta := metav1.ObjectMeta{
		Namespace: ns,
		Name:      cr.PrefixedName(),
	}
	objsToRemove := []client.Object{
		&appsv1.Deployment{ObjectMeta: objMeta},
		&corev1.Service{ObjectMeta: objMeta},
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{
			Name:      cr.GetServiceAccountName(),
			Namespace: ns,
		}},
		&corev1.Secret{ObjectMeta: objMeta},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{
			Name:      build.ResourceName(build.TLSAssetsResourceKind, cr),
			Namespace: ns,
		}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{
			Name:      build.ResourceName(build.RelabelConfigResourceKind, cr),
			Namespace: ns,
		}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{
			Name:      build.ResourceName(build.StreamAggrConfigResourceKind, cr),
			Namespace: ns,
		}},
		&rbacv1.Role{ObjectMeta: metav1.ObjectMeta{
			Name:      cr.GetRBACName(),
			Namespace: ns,
		}},
		&rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{
			Name:      cr.GetRBACName(),
			Namespace: ns,
		}},
	}
	if cr.Spec.ServiceSpec != nil {
		objsToRemove = append(objsToRemove, &corev1.Service{ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Spec.ServiceSpec.NameOrDefault(cr.PrefixedName()),
			Namespace: ns,
		}})
	}
	if cr.Spec.AdditionalScrapeConfigs != nil {
		objsToRemove = append(objsToRemove, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Spec.AdditionalScrapeConfigs.Name,
			Namespace: ns,
		}})
	}
	deleteOwnerReferences := make([]bool, len(objsToRemove))

	deleteOwnerReferences = append(deleteOwnerReferences, !cr.Spec.RemovePvcAfterDelete)
	objsToRemove = append(objsToRemove, &corev1.PersistentVolumeClaim{ObjectMeta: objMeta})

	deleteOwnerReferences = append(deleteOwnerReferences, false)
	objsToRemove = append(objsToRemove, cr)

	return removeFinalizers(ctx, rclient, objsToRemove, deleteOwnerReferences, cr)
}
