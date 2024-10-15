package reconcile

import (
	"context"
	"fmt"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"

	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RoleBinding reconciles rolebindg object
func RoleBinding(ctx context.Context, rclient client.Client, rb *rbacv1.RoleBinding) error {
	var existRoleBinding rbacv1.RoleBinding
	if err := rclient.Get(ctx, types.NamespacedName{Namespace: rb.Namespace, Name: rb.Name}, &existRoleBinding); err != nil {
		if errors.IsNotFound(err) {
			return rclient.Create(ctx, rb)
		}
		return fmt.Errorf("cannot get exist rolebinding: %w", err)
	}
	if err := finalize.FreeIfNeeded(ctx, rclient, &existRoleBinding); err != nil {
		return err
	}
	existRoleBinding.Annotations = labels.Merge(existRoleBinding.Annotations, rb.Annotations)
	existRoleBinding.OwnerReferences = rb.OwnerReferences

	if equality.Semantic.DeepEqual(rb.Subjects, existRoleBinding.Subjects) &&
		equality.Semantic.DeepEqual(rb.RoleRef, existRoleBinding.RoleRef) &&
		equality.Semantic.DeepEqual(rb.Labels, existRoleBinding.Labels) &&
		equality.Semantic.DeepEqual(rb.Annotations, existRoleBinding.Annotations) &&
		equality.Semantic.DeepEqual(rb.OwnerReferences, existRoleBinding.OwnerReferences) {
		return nil
	}
	logger.WithContext(ctx).Info("updating rolebinding configuration", "rolebinding_name", rb.Name)

	existRoleBinding.Labels = rb.Labels
	existRoleBinding.Subjects = rb.Subjects
	existRoleBinding.RoleRef = rb.RoleRef
	existRoleBinding.OwnerReferences = rb.OwnerReferences
	vmv1beta1.AddFinalizer(&existRoleBinding, &existRoleBinding)

	return rclient.Update(ctx, &existRoleBinding)
}

// Role reconciles role object
func Role(ctx context.Context, rclient client.Client, rl *rbacv1.Role) error {
	var existRole rbacv1.Role
	if err := rclient.Get(ctx, types.NamespacedName{Namespace: rl.Namespace, Name: rl.Name}, &existRole); err != nil {
		if errors.IsNotFound(err) {
			return rclient.Create(ctx, rl)
		}
		return fmt.Errorf("cannot get exist role: %w", err)
	}
	if err := finalize.FreeIfNeeded(ctx, rclient, &existRole); err != nil {
		return err
	}
	existRole.Annotations = labels.Merge(existRole.Annotations, rl.Annotations)

	if equality.Semantic.DeepEqual(rl.Rules, existRole.Rules) &&
		equality.Semantic.DeepEqual(rl.Labels, existRole.Labels) &&
		equality.Semantic.DeepEqual(rl.Annotations, existRole.Annotations) &&
		equality.Semantic.DeepEqual(rl.OwnerReferences, existRole.OwnerReferences) {
		return nil
	}
	logger.WithContext(ctx).Info("updating role configuration", "role_name", rl.Name)

	existRole.Labels = rl.Labels
	existRole.Rules = rl.Rules
	existRole.OwnerReferences = rl.OwnerReferences
	vmv1beta1.AddFinalizer(&existRole, &existRole)

	return rclient.Update(ctx, &existRole)
}
