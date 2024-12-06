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
func RoleBinding(ctx context.Context, rclient client.Client, newRB, prevRB *rbacv1.RoleBinding) error {
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
	if prevRB != nil {
		prevAnnotations = prevRB.Annotations
	}

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
func Role(ctx context.Context, rclient client.Client, newRL, prevRL *rbacv1.Role) error {
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
	if prevRL != nil {
		prevAnnotations = prevRL.Annotations
	}

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

// ClusterRoleBinding reconciles cluster role binding object
func ClusterRoleBinding(ctx context.Context, rclient client.Client, newCRB, prevCRB *rbacv1.ClusterRoleBinding) error {
	var currentCRB rbacv1.ClusterRoleBinding

	if err := rclient.Get(ctx, types.NamespacedName{Name: newCRB.Name, Namespace: newCRB.Namespace}, &currentCRB); err != nil {
		if errors.IsNotFound(err) {
			return rclient.Create(ctx, newCRB)
		}
		return fmt.Errorf("cannot get crb: %w", err)
	}
	if err := finalize.FreeIfNeeded(ctx, rclient, &currentCRB); err != nil {
		return err
	}

	var prevAnnotations map[string]string
	if prevCRB != nil {
		prevAnnotations = prevCRB.Annotations
	}
	// fast path
	if equality.Semantic.DeepEqual(newCRB.Subjects, currentCRB.Subjects) &&
		equality.Semantic.DeepEqual(newCRB.RoleRef, currentCRB.RoleRef) &&
		equality.Semantic.DeepEqual(newCRB.Labels, currentCRB.Labels) &&
		isAnnotationsEqual(currentCRB.Annotations, newCRB.Annotations, prevAnnotations) {
		return nil
	}
	logger.WithContext(ctx).Info("updating ClusterRoleBinding")

	currentCRB.OwnerReferences = newCRB.OwnerReferences
	currentCRB.Labels = newCRB.Labels
	currentCRB.Annotations = mergeAnnotations(currentCRB.Annotations, newCRB.Annotations, prevAnnotations)
	currentCRB.Subjects = newCRB.Subjects
	currentCRB.RoleRef = newCRB.RoleRef
	vmv1beta1.AddFinalizer(&currentCRB, &currentCRB)
	return rclient.Update(ctx, &currentCRB)

}

func ClusterRole(ctx context.Context, rclient client.Client, newClusterRole, prevClusterRole *rbacv1.ClusterRole) error {
	var currentClusterRole rbacv1.ClusterRole

	if err := rclient.Get(ctx, types.NamespacedName{Name: newClusterRole.Name, Namespace: newClusterRole.Namespace}, &currentClusterRole); err != nil {
		if errors.IsNotFound(err) {
			return rclient.Create(ctx, newClusterRole)
		}
		return fmt.Errorf("cannot get exist cluster role: %w", err)
	}
	if err := finalize.FreeIfNeeded(ctx, rclient, &currentClusterRole); err != nil {
		return err
	}
	var prevAnnotations map[string]string
	if prevClusterRole != nil {
		prevAnnotations = prevClusterRole.Annotations
	}
	// fast path
	if equality.Semantic.DeepEqual(newClusterRole.Rules, currentClusterRole.Rules) &&
		equality.Semantic.DeepEqual(newClusterRole.Labels, currentClusterRole.Labels) &&
		isAnnotationsEqual(currentClusterRole.Annotations, newClusterRole.Annotations, prevAnnotations) {
		return nil
	}
	logger.WithContext(ctx).Info("updating ClusterRole")

	currentClusterRole.OwnerReferences = newClusterRole.OwnerReferences
	currentClusterRole.Labels = newClusterRole.Labels
	currentClusterRole.Annotations = mergeAnnotations(currentClusterRole.Annotations, newClusterRole.Annotations, prevAnnotations)
	currentClusterRole.Rules = newClusterRole.Rules
	vmv1beta1.AddFinalizer(&currentClusterRole, &currentClusterRole)
	return rclient.Update(ctx, &currentClusterRole)
}
