package vmauth

import (
	"context"
	"fmt"

	v1beta12 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/controllers/factory/k8stools"
	v1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func CreateVMAuthSecretAccess(ctx context.Context, cr *v1beta12.VMAuth, rclient client.Client) error {
	if err := ensureVMAuthRoleExist(ctx, cr, rclient); err != nil {
		return fmt.Errorf("cannot check vmauth role: %w", err)
	}
	if err := ensureVMAgentRBExist(ctx, cr, rclient); err != nil {
		return fmt.Errorf("cannot check vmauth role binding: %w", err)
	}
	return nil
}

func ensureVMAuthRoleExist(ctx context.Context, cr *v1beta12.VMAuth, rclient client.Client) error {
	role := buildVMAuthRole(cr)
	var existRole v1.Role
	err := k8stools.ListClusterWideObjects(ctx, rclient, &v1.RoleList{}, cr.Labels(), func(r runtime.Object) {
		items := r.(*v1.RoleList)
		for _, i := range items.Items {
			if i.Name == role.Name {
				existRole = i
				return
			}
		}
	})
	if err != nil {
		return err
	}
	if existRole.Name == "" {
		return rclient.Create(ctx, role)
	}
	existRole.OwnerReferences = role.OwnerReferences
	existRole.Labels = labels.Merge(existRole.Labels, role.Labels)
	existRole.Annotations = labels.Merge(role.Annotations, existRole.Annotations)
	existRole.Rules = role.Rules
	v1beta12.MergeFinalizers(&existRole, v1beta12.FinalizerName)
	return rclient.Update(ctx, &existRole)
}

func ensureVMAgentRBExist(ctx context.Context, cr *v1beta12.VMAuth, rclient client.Client) error {
	roleBinding := buildVMAuthRoleBinding(cr)
	var existRoleBinding v1.RoleBinding
	err := k8stools.ListClusterWideObjects(ctx, rclient, &v1.RoleBindingList{}, cr.Labels(), func(r runtime.Object) {
		items := r.(*v1.RoleBindingList)
		for _, i := range items.Items {
			if i.Name == roleBinding.Name {
				existRoleBinding = i
				return
			}
		}
	})
	if err != nil {
		return err
	}
	if existRoleBinding.Name == "" {
		return rclient.Create(ctx, roleBinding)
	}

	existRoleBinding.OwnerReferences = roleBinding.OwnerReferences
	existRoleBinding.Labels = labels.Merge(existRoleBinding.Labels, roleBinding.Labels)
	existRoleBinding.Annotations = labels.Merge(roleBinding.Annotations, existRoleBinding.Annotations)
	existRoleBinding.Subjects = roleBinding.Subjects
	existRoleBinding.RoleRef = roleBinding.RoleRef
	v1beta12.MergeFinalizers(&existRoleBinding, v1beta12.FinalizerName)
	return rclient.Update(ctx, &existRoleBinding)
}

func buildVMAuthRole(cr *v1beta12.VMAuth) *v1.Role {

	return &v1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(),
			Namespace:       cr.Namespace,
			Labels:          cr.Labels(),
			Annotations:     cr.Annotations(),
			Finalizers:      []string{v1beta12.FinalizerName},
			OwnerReferences: cr.AsOwner(),
		},
		Rules: []v1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"secrets"},
				Verbs:     []string{"get", "list", "watch"},
			},
		},
	}
}

func buildVMAuthRoleBinding(cr *v1beta12.VMAuth) *v1.RoleBinding {
	return &v1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(),
			Namespace:       cr.Namespace,
			Labels:          cr.Labels(),
			Annotations:     cr.Annotations(),
			Finalizers:      []string{v1beta12.FinalizerName},
			OwnerReferences: cr.AsOwner(),
		},
		RoleRef: v1.RoleRef{
			Name:     cr.PrefixedName(),
			Kind:     "Role",
			APIGroup: "rbac.authorization.k8s.io",
		},
		Subjects: []v1.Subject{
			{
				Name:      cr.GetServiceAccountName(),
				Namespace: cr.Namespace,
				Kind:      "ServiceAccount",
			},
		},
	}
}
