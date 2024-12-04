package reconcile

import (
	"context"
	"fmt"

	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

// RoleBinding reconciles rolebindg object
// TODO: @f41gh7 add prevCR
func RoleBinding(ctx context.Context, rclient client.Client, newRB *rbacv1.RoleBinding) error {
	var currentRB rbacv1.RoleBinding
	if err := rclient.Get(ctx, types.NamespacedName{Namespace: newRB.Namespace, Name: newRB.Name}, &currentRB); err != nil {
		if errors.IsNotFound(err) {
			return rclient.Create(ctx, newRB)
		}
		return fmt.Errorf("cannot get exist rolebinding: %w", err)
	}
	if err := finalize.FreeIfNeeded(ctx, rclient, &currentRB); err != nil {
		return err
	}

	var prevAnnotations map[string]string

	if equality.Semantic.DeepEqual(newRB.Subjects, currentRB.Subjects) &&
		equality.Semantic.DeepEqual(newRB.RoleRef, currentRB.RoleRef) &&
		equality.Semantic.DeepEqual(newRB.Labels, currentRB.Labels) &&
		isAnnotationsEqual(currentRB.Annotations, newRB.Annotations, prevAnnotations) {
		return nil
	}
	logger.WithContext(ctx).Info("updating rolebinding configuration", "rolebinding_name", newRB.Name)

	currentRB.Annotations = mergeAnnotations(currentRB.Annotations, newRB.Annotations, prevAnnotations)
	currentRB.Labels = newRB.Labels
	currentRB.Subjects = newRB.Subjects
	currentRB.RoleRef = newRB.RoleRef
	currentRB.OwnerReferences = newRB.OwnerReferences
	vmv1beta1.AddFinalizer(&currentRB, &currentRB)

	return rclient.Update(ctx, &currentRB)
}

// Role reconciles role object
// TODO: @f41gh7 add prevCR
func Role(ctx context.Context, rclient client.Client, newRL *rbacv1.Role) error {
	var currentRL rbacv1.Role
	if err := rclient.Get(ctx, types.NamespacedName{Namespace: newRL.Namespace, Name: newRL.Name}, &currentRL); err != nil {
		if errors.IsNotFound(err) {
			return rclient.Create(ctx, newRL)
		}
		return fmt.Errorf("cannot get exist role: %w", err)
	}
	if err := finalize.FreeIfNeeded(ctx, rclient, &currentRL); err != nil {
		return err
	}
	var prevAnnotations map[string]string

	if equality.Semantic.DeepEqual(newRL.Rules, currentRL.Rules) &&
		equality.Semantic.DeepEqual(newRL.Labels, currentRL.Labels) &&
		isAnnotationsEqual(currentRL.Annotations, newRL.Annotations, prevAnnotations) {
		return nil
	}
	logger.WithContext(ctx).Info("updating role configuration", "role_name", newRL.Name)

	currentRL.Annotations = mergeAnnotations(currentRL.Annotations, newRL.Annotations, prevAnnotations)
	currentRL.Labels = newRL.Labels
	currentRL.Rules = newRL.Rules
	currentRL.OwnerReferences = newRL.OwnerReferences
	vmv1beta1.AddFinalizer(&currentRL, &currentRL)

	return rclient.Update(ctx, &currentRL)
}
