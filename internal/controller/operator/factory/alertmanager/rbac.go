package alertmanager

import (
	"context"
	"fmt"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"

	corev1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// createVMAlertmanagerSecretAccess creates k8s api access for vmalertmanager config-reloader container
func createVMAlertmanagerSecretAccess(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAlertmanager) error {
	if err := ensureVMAlertmanagerRoleExist(ctx, cr, rclient); err != nil {
		return fmt.Errorf("cannot check vmauth role: %w", err)
	}
	if err := ensureVMAlertmanagerRBExist(ctx, cr, rclient); err != nil {
		return fmt.Errorf("cannot check vmauth role binding: %w", err)
	}
	return nil
}

func ensureVMAlertmanagerRoleExist(ctx context.Context, cr *vmv1beta1.VMAlertmanager, rclient client.Client) error {
	role := buildVMAlertmanagerRole(cr)
	return reconcile.Role(ctx, rclient, role)
}

func ensureVMAlertmanagerRBExist(ctx context.Context, cr *vmv1beta1.VMAlertmanager, rclient client.Client) error {
	roleBinding := buildVMAlertmanagerRoleBinding(cr)
	return reconcile.RoleBinding(ctx, rclient, roleBinding)
}

func buildVMAlertmanagerRole(cr *vmv1beta1.VMAlertmanager) *corev1.Role {
	return &corev1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(),
			Namespace:       cr.Namespace,
			Labels:          cr.AllLabels(),
			Annotations:     cr.AnnotationsFiltered(),
			Finalizers:      []string{vmv1beta1.FinalizerName},
			OwnerReferences: cr.AsOwner(),
		},
		Rules: []corev1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"secrets"},
				Verbs:     []string{"get", "list", "watch"},
			},
		},
	}
}

func buildVMAlertmanagerRoleBinding(cr *vmv1beta1.VMAlertmanager) *corev1.RoleBinding {
	return &corev1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(),
			Namespace:       cr.Namespace,
			Labels:          cr.AllLabels(),
			Annotations:     cr.AnnotationsFiltered(),
			Finalizers:      []string{vmv1beta1.FinalizerName},
			OwnerReferences: cr.AsOwner(),
		},
		RoleRef: corev1.RoleRef{
			Name:     cr.PrefixedName(),
			Kind:     "Role",
			APIGroup: "rbac.authorization.k8s.io",
		},
		Subjects: []corev1.Subject{
			{
				Name:      cr.GetServiceAccountName(),
				Namespace: cr.Namespace,
				Kind:      "ServiceAccount",
			},
		},
	}
}
