package vmalertmanager

import (
	"context"
	"fmt"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

// createConfigSecretAccess creates k8s api access for vmalertmanager config-reloader container
func createConfigSecretAccess(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMAlertmanager) error {
	if err := ensureVMAlertmanagerRoleExist(ctx, rclient, cr, prevCR); err != nil {
		return fmt.Errorf("cannot check vmauth role: %w", err)
	}
	if err := ensureVMAlertmanagerRBExist(ctx, rclient, cr, prevCR); err != nil {
		return fmt.Errorf("cannot check vmauth role binding: %w", err)
	}
	return nil
}

func ensureVMAlertmanagerRoleExist(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMAlertmanager) error {
	var prevRole *rbacv1.Role
	if prevCR != nil {
		prevRole = buildRole(prevCR)
	}
	owner := cr.AsOwner()
	return reconcile.Role(ctx, rclient, buildRole(cr), prevRole, &owner)
}

func ensureVMAlertmanagerRBExist(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMAlertmanager) error {
	var prevRB *rbacv1.RoleBinding
	if prevCR != nil {
		prevRB = buildRoleBinding(prevCR)
	}
	owner := cr.AsOwner()
	return reconcile.RoleBinding(ctx, rclient, buildRoleBinding(cr), prevRB, &owner)
}

func buildRole(cr *vmv1beta1.VMAlertmanager) *rbacv1.Role {
	return &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(),
			Namespace:       cr.Namespace,
			Labels:          cr.FinalLabels(),
			Annotations:     cr.FinalAnnotations(),
			OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"secrets"},
				Verbs:     []string{"get", "list", "watch"},
			},
		},
	}
}

func buildRoleBinding(cr *vmv1beta1.VMAlertmanager) *rbacv1.RoleBinding {
	return &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(),
			Namespace:       cr.Namespace,
			Labels:          cr.FinalLabels(),
			Annotations:     cr.FinalAnnotations(),
			OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
		},
		RoleRef: rbacv1.RoleRef{
			Name:     cr.PrefixedName(),
			Kind:     "Role",
			APIGroup: "rbac.authorization.k8s.io",
		},
		Subjects: []rbacv1.Subject{
			{
				Name:      cr.GetServiceAccountName(),
				Namespace: cr.Namespace,
				Kind:      "ServiceAccount",
			},
		},
	}
}
