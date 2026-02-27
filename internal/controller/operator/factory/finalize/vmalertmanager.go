package finalize

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// OnVMAlertManagerDelete deletes all alertmanager related resources
func OnVMAlertManagerDelete(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAlertmanager) error {
	ns := cr.GetNamespace()
	objMeta := metav1.ObjectMeta{
		Namespace: ns,
		Name:      cr.PrefixedName(),
	}
	objsToRemove := []client.Object{
		&appsv1.StatefulSet{ObjectMeta: objMeta},
		&corev1.Service{ObjectMeta: objMeta},
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{
			Name:      cr.GetServiceAccountName(),
			Namespace: ns,
		}},
		&policyv1.PodDisruptionBudget{ObjectMeta: objMeta},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{
			Name:      cr.ConfigSecretName(),
			Namespace: ns,
		}},
		&rbacv1.RoleBinding{ObjectMeta: objMeta},
		&rbacv1.Role{ObjectMeta: objMeta},
	}
	if cr.Spec.ServiceSpec != nil {
		objsToRemove = append(objsToRemove, &corev1.Service{ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Spec.ServiceSpec.NameOrDefault(cr.PrefixedName()),
			Namespace: ns,
		}})
	}
	if len(cr.Spec.ConfigSecret) > 0 {
		objsToRemove = append(objsToRemove, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Spec.ConfigSecret,
			Namespace: ns,
		}})
	}
	objsToRemove = append(objsToRemove, cr)
	deleteOwnerReferences := make([]bool, len(objsToRemove))
	return removeFinalizers(ctx, rclient, objsToRemove, deleteOwnerReferences, cr)
}
