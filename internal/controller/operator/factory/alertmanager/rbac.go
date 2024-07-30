package alertmanager

import (
	"context"
	"fmt"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	corev1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
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
	var existRole corev1.Role
	if err := rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: role.Name}, &existRole); err != nil {
		if errors.IsNotFound(err) {
			return rclient.Create(ctx, role)
		}
		return fmt.Errorf("cannot get role for vmauth: %w", err)
	}

	existRole.OwnerReferences = role.OwnerReferences
	existRole.Labels = role.Labels
	existRole.Annotations = labels.Merge(existRole.Annotations, role.Annotations)
	existRole.Rules = role.Rules
	vmv1beta1.AddFinalizer(&existRole, &existRole)
	return rclient.Update(ctx, &existRole)
}

func ensureVMAlertmanagerRBExist(ctx context.Context, cr *vmv1beta1.VMAlertmanager, rclient client.Client) error {
	roleBinding := buildVMAlertmanagerRoleBinding(cr)
	var existRoleBinding corev1.RoleBinding
	if err := rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: roleBinding.Name}, &existRoleBinding); err != nil {
		if errors.IsNotFound(err) {
			return rclient.Create(ctx, roleBinding)
		}
		return fmt.Errorf("cannot get rolebinding for vmauth: %w", err)
	}

	existRoleBinding.OwnerReferences = roleBinding.OwnerReferences
	existRoleBinding.Labels = roleBinding.Labels
	existRoleBinding.Annotations = labels.Merge(existRoleBinding.Annotations, roleBinding.Annotations)
	existRoleBinding.Subjects = roleBinding.Subjects
	existRoleBinding.RoleRef = roleBinding.RoleRef
	vmv1beta1.AddFinalizer(&existRoleBinding, &existRoleBinding)
	return rclient.Update(ctx, &existRoleBinding)
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
