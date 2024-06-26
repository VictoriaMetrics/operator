package vmauth

import (
	"context"
	"fmt"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// createVMAuthSecretAccess creates rbac rule for watching secret changes with vmauth configuration
func createVMAuthSecretAccess(ctx context.Context, cr *vmv1beta1.VMAuth, rclient client.Client) error {
	if err := ensureVMAuthRoleExist(ctx, cr, rclient); err != nil {
		return fmt.Errorf("cannot check vmauth role: %w", err)
	}
	if err := ensureVMAgentRBExist(ctx, cr, rclient); err != nil {
		return fmt.Errorf("cannot check vmauth role binding: %w", err)
	}
	return nil
}

func ensureVMAuthRoleExist(ctx context.Context, cr *vmv1beta1.VMAuth, rclient client.Client) error {
	role := buildVMAuthRole(cr)
	var existRole rbacv1.Role
	if err := rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: role.Name}, &existRole); err != nil {
		if errors.IsNotFound(err) {
			return rclient.Create(ctx, role)
		}
		return fmt.Errorf("cannot get role for vmauth: %w", err)
	}
	if err := finalize.FreeIfNeeded(ctx, rclient, &existRole); err != nil {
		return err
	}

	existRole.OwnerReferences = role.OwnerReferences
	existRole.Labels = role.Labels
	existRole.Annotations = labels.Merge(existRole.Annotations, role.Annotations)
	existRole.Rules = role.Rules
	vmv1beta1.MergeFinalizers(&existRole, vmv1beta1.FinalizerName)
	return rclient.Update(ctx, &existRole)
}

func ensureVMAgentRBExist(ctx context.Context, cr *vmv1beta1.VMAuth, rclient client.Client) error {
	roleBinding := buildVMAuthRoleBinding(cr)
	var existRoleBinding rbacv1.RoleBinding
	if err := rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: roleBinding.Name}, &existRoleBinding); err != nil {
		if errors.IsNotFound(err) {
			return rclient.Create(ctx, roleBinding)
		}
		return fmt.Errorf("cannot get rolebinding for vmauth: %w", err)
	}
	if err := finalize.FreeIfNeeded(ctx, rclient, &existRoleBinding); err != nil {
		return err
	}

	existRoleBinding.OwnerReferences = roleBinding.OwnerReferences
	existRoleBinding.Labels = roleBinding.Labels
	existRoleBinding.Annotations = labels.Merge(existRoleBinding.Annotations, roleBinding.Annotations)
	existRoleBinding.Subjects = roleBinding.Subjects
	existRoleBinding.RoleRef = roleBinding.RoleRef
	vmv1beta1.MergeFinalizers(&existRoleBinding, vmv1beta1.FinalizerName)
	return rclient.Update(ctx, &existRoleBinding)
}

func buildVMAuthRole(cr *vmv1beta1.VMAuth) *rbacv1.Role {
	return &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(),
			Namespace:       cr.Namespace,
			Labels:          cr.AllLabels(),
			Annotations:     cr.AnnotationsFiltered(),
			Finalizers:      []string{vmv1beta1.FinalizerName},
			OwnerReferences: cr.AsOwner(),
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

func buildVMAuthRoleBinding(cr *vmv1beta1.VMAuth) *rbacv1.RoleBinding {
	return &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(),
			Namespace:       cr.Namespace,
			Labels:          cr.AllLabels(),
			Annotations:     cr.AnnotationsFiltered(),
			Finalizers:      []string{vmv1beta1.FinalizerName},
			OwnerReferences: cr.AsOwner(),
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
