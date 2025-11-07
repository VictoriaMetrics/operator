package reconcile

import (
	"context"
	"fmt"

	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
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
		if k8serrors.IsNotFound(err) {
			logger.WithContext(ctx).Info(fmt.Sprintf("creating new RoleBinding %s", newRB.Name))
			return rclient.Create(ctx, newRB)
		}
		return fmt.Errorf("cannot get exist rolebinding: %w", err)
	}
	if !currentRB.DeletionTimestamp.IsZero() {
		return &errRecreate{
			origin: fmt.Errorf("waiting for rolebinding %q to be removed", newRB.Name),
		}
	}
	if err := finalize.FreeIfNeeded(ctx, rclient, &currentRB); err != nil {
		return err
	}

	if equality.Semantic.DeepEqual(newRB.Subjects, currentRB.Subjects) &&
		equality.Semantic.DeepEqual(newRB.RoleRef, currentRB.RoleRef) &&
		isObjectMetaEqual(&currentRB, newRB, prevRB) {
		return nil
	}
	logger.WithContext(ctx).Info(fmt.Sprintf("updating RoleBinding %s configuration", newRB.Name))
	mergeObjectMetadataIntoNew(&currentRB, newRB, prevRB)
	vmv1beta1.AddFinalizer(newRB, &currentRB)

	return rclient.Update(ctx, newRB)
}

// Role reconciles role object
func Role(ctx context.Context, rclient client.Client, newRL, prevRL *rbacv1.Role) error {
	var currentRL rbacv1.Role
	if err := rclient.Get(ctx, types.NamespacedName{Namespace: newRL.Namespace, Name: newRL.Name}, &currentRL); err != nil {
		if k8serrors.IsNotFound(err) {
			logger.WithContext(ctx).Info(fmt.Sprintf("creating new Role %s", newRL.Name))
			return rclient.Create(ctx, newRL)
		}
		return fmt.Errorf("cannot get exist role: %w", err)
	}
	if !currentRL.DeletionTimestamp.IsZero() {
		return &errRecreate{
			origin: fmt.Errorf("waiting for role %q to be removed", newRL.Name),
		}
	}
	if err := finalize.FreeIfNeeded(ctx, rclient, &currentRL); err != nil {
		return err
	}

	if equality.Semantic.DeepEqual(newRL.Rules, currentRL.Rules) &&
		isObjectMetaEqual(&currentRL, newRL, prevRL) {
		return nil
	}
	logger.WithContext(ctx).Info(fmt.Sprintf("updating Role %s configuration", newRL.Name))
	mergeObjectMetadataIntoNew(&currentRL, newRL, prevRL)
	vmv1beta1.AddFinalizer(newRL, &currentRL)

	return rclient.Update(ctx, newRL)
}

// ClusterRoleBinding reconciles cluster role binding object
func ClusterRoleBinding(ctx context.Context, rclient client.Client, newCRB, prevCRB *rbacv1.ClusterRoleBinding) error {
	var currentCRB rbacv1.ClusterRoleBinding

	if err := rclient.Get(ctx, types.NamespacedName{Name: newCRB.Name, Namespace: newCRB.Namespace}, &currentCRB); err != nil {
		if k8serrors.IsNotFound(err) {
			logger.WithContext(ctx).Info(fmt.Sprintf("creating new ClusterRoleBinding %s", newCRB.Name))
			return rclient.Create(ctx, newCRB)
		}
		return fmt.Errorf("cannot get crb: %w", err)
	}
	if !currentCRB.DeletionTimestamp.IsZero() {
		return &errRecreate{
			origin: fmt.Errorf("waiting for clusterrolebinding %q to be removed", newCRB.Name),
		}
	}
	if err := finalize.FreeIfNeeded(ctx, rclient, &currentCRB); err != nil {
		return err
	}

	// fast path
	if equality.Semantic.DeepEqual(newCRB.Subjects, currentCRB.Subjects) &&
		equality.Semantic.DeepEqual(newCRB.RoleRef, currentCRB.RoleRef) &&
		isObjectMetaEqual(&currentCRB, newCRB, prevCRB) {
		return nil
	}
	logger.WithContext(ctx).Info(fmt.Sprintf("updating ClusterRoleBinding %s", newCRB.Name))
	mergeObjectMetadataIntoNew(&currentCRB, newCRB, prevCRB)
	vmv1beta1.AddFinalizer(newCRB, &currentCRB)

	return rclient.Update(ctx, newCRB)

}

// ClusterRole reconciles cluster role object
func ClusterRole(ctx context.Context, rclient client.Client, newClusterRole, prevClusterRole *rbacv1.ClusterRole) error {
	var currentClusterRole rbacv1.ClusterRole

	if err := rclient.Get(ctx, types.NamespacedName{Name: newClusterRole.Name, Namespace: newClusterRole.Namespace}, &currentClusterRole); err != nil {
		if k8serrors.IsNotFound(err) {
			logger.WithContext(ctx).Info(fmt.Sprintf("creating new ClusterRole %s", newClusterRole.Name))
			return rclient.Create(ctx, newClusterRole)
		}
		return fmt.Errorf("cannot get exist cluster role: %w", err)
	}
	if !currentClusterRole.DeletionTimestamp.IsZero() {
		return &errRecreate{
			origin: fmt.Errorf("waiting for clusterrole %q to be removed", newClusterRole.Name),
		}
	}
	if err := finalize.FreeIfNeeded(ctx, rclient, &currentClusterRole); err != nil {
		return err
	}
	// fast path
	if equality.Semantic.DeepEqual(newClusterRole.Rules, currentClusterRole.Rules) &&
		isObjectMetaEqual(&currentClusterRole, newClusterRole, prevClusterRole) {
		return nil
	}
	logger.WithContext(ctx).Info(fmt.Sprintf("updating ClusterRole %s", newClusterRole.Name))

	mergeObjectMetadataIntoNew(&currentClusterRole, newClusterRole, prevClusterRole)
	vmv1beta1.AddFinalizer(newClusterRole, &currentClusterRole)
	return rclient.Update(ctx, newClusterRole)
}
