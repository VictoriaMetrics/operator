package reconcile

import (
	"context"
	"fmt"

	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

// RoleBinding reconciles rolebindg object
func RoleBinding(ctx context.Context, rclient client.Client, newObj, prevObj *rbacv1.RoleBinding) error {
	var existingObj rbacv1.RoleBinding
	nsn := types.NamespacedName{Name: newObj.Name, Namespace: newObj.Namespace}
	if err := rclient.Get(ctx, nsn, &existingObj); err != nil {
		if k8serrors.IsNotFound(err) {
			logger.WithContext(ctx).Info(fmt.Sprintf("creating new RoleBinding=%s", nsn))
			return rclient.Create(ctx, newObj)
		}
		return fmt.Errorf("cannot get exist RoleBinding=%s: %w", nsn, err)
	}
	if err := freeIfNeeded(ctx, rclient, &existingObj); err != nil {
		return err
	}
	var prevMeta *metav1.ObjectMeta
	if prevObj != nil {
		prevMeta = &prevObj.ObjectMeta
	}
	if equality.Semantic.DeepEqual(newObj.Subjects, existingObj.Subjects) &&
		equality.Semantic.DeepEqual(newObj.RoleRef, existingObj.RoleRef) &&
		isObjectMetaEqual(&existingObj, newObj, prevMeta) {
		return nil
	}
	logger.WithContext(ctx).Info(fmt.Sprintf("updating RoleBinding=%s configuration", nsn))
	mergeObjectMetadataIntoNew(&existingObj, newObj, prevMeta)
	vmv1beta1.AddFinalizer(newObj, &existingObj)

	return rclient.Update(ctx, newObj)
}

// Role reconciles role object
func Role(ctx context.Context, rclient client.Client, newObj, prevObj *rbacv1.Role) error {
	var existingObj rbacv1.Role
	nsn := types.NamespacedName{Name: newObj.Name, Namespace: newObj.Namespace}
	if err := rclient.Get(ctx, nsn, &existingObj); err != nil {
		if k8serrors.IsNotFound(err) {
			logger.WithContext(ctx).Info(fmt.Sprintf("creating new Role=%s", nsn))
			return rclient.Create(ctx, newObj)
		}
		return fmt.Errorf("cannot get exist Role=%s: %w", nsn, err)
	}
	if err := freeIfNeeded(ctx, rclient, &existingObj); err != nil {
		return err
	}
	var prevMeta *metav1.ObjectMeta
	if prevObj != nil {
		prevMeta = &prevObj.ObjectMeta
	}
	if equality.Semantic.DeepEqual(newObj.Rules, existingObj.Rules) &&
		isObjectMetaEqual(&existingObj, newObj, prevMeta) {
		return nil
	}
	logger.WithContext(ctx).Info(fmt.Sprintf("updating Role=%s configuration", nsn))
	mergeObjectMetadataIntoNew(&existingObj, newObj, prevMeta)
	vmv1beta1.AddFinalizer(newObj, &existingObj)

	return rclient.Update(ctx, newObj)
}

// ClusterRoleBinding reconciles cluster role binding object
func ClusterRoleBinding(ctx context.Context, rclient client.Client, newObj, prevObj *rbacv1.ClusterRoleBinding) error {
	var existingObj rbacv1.ClusterRoleBinding
	nsn := types.NamespacedName{Name: newObj.Name, Namespace: newObj.Namespace}
	if err := rclient.Get(ctx, nsn, &existingObj); err != nil {
		if k8serrors.IsNotFound(err) {
			logger.WithContext(ctx).Info(fmt.Sprintf("creating new ClusterRoleBinding=%s", newObj.Name))
			return rclient.Create(ctx, newObj)
		}
		return fmt.Errorf("cannot get ClusterRoleBinding=%s: %w", newObj.Name, err)
	}
	if err := freeIfNeeded(ctx, rclient, &existingObj); err != nil {
		return err
	}
	var prevMeta *metav1.ObjectMeta
	if prevObj != nil {
		prevMeta = &prevObj.ObjectMeta
	}

	// fast path
	if equality.Semantic.DeepEqual(newObj.Subjects, existingObj.Subjects) &&
		equality.Semantic.DeepEqual(newObj.RoleRef, existingObj.RoleRef) &&
		isObjectMetaEqual(&existingObj, newObj, prevMeta) {
		return nil
	}
	logger.WithContext(ctx).Info(fmt.Sprintf("updating ClusterRoleBinding=%s", newObj.Name))
	mergeObjectMetadataIntoNew(&existingObj, newObj, prevMeta)
	vmv1beta1.AddFinalizer(newObj, &existingObj)

	return rclient.Update(ctx, newObj)

}

// ClusterRole reconciles cluster role object
func ClusterRole(ctx context.Context, rclient client.Client, newObj, prevObj *rbacv1.ClusterRole) error {
	var existingObj rbacv1.ClusterRole
	nsn := types.NamespacedName{Name: newObj.Name, Namespace: newObj.Namespace}
	if err := rclient.Get(ctx, nsn, &existingObj); err != nil {
		if k8serrors.IsNotFound(err) {
			logger.WithContext(ctx).Info(fmt.Sprintf("creating new ClusterRole=%s", newObj.Name))
			return rclient.Create(ctx, newObj)
		}
		return fmt.Errorf("cannot get exist ClusterRole=%s: %w", newObj.Name, err)
	}
	if err := freeIfNeeded(ctx, rclient, &existingObj); err != nil {
		return err
	}
	var prevMeta *metav1.ObjectMeta
	if prevObj != nil {
		prevMeta = &prevObj.ObjectMeta
	}

	// fast path
	if equality.Semantic.DeepEqual(newObj.Rules, existingObj.Rules) &&
		isObjectMetaEqual(&existingObj, newObj, prevMeta) {
		return nil
	}
	logger.WithContext(ctx).Info(fmt.Sprintf("updating ClusterRole=%s", newObj.Name))

	mergeObjectMetadataIntoNew(&existingObj, newObj, prevMeta)
	vmv1beta1.AddFinalizer(newObj, &existingObj)
	return rclient.Update(ctx, newObj)
}
