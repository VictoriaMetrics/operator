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

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

// RoleBinding reconciles rolebindg object
func RoleBinding(ctx context.Context, rclient client.Client, newObj, prevObj *rbacv1.RoleBinding, owner *metav1.OwnerReference) error {
	nsn := types.NamespacedName{Name: newObj.Name, Namespace: newObj.Namespace}
	var prevMeta *metav1.ObjectMeta
	if prevObj != nil {
		prevMeta = &prevObj.ObjectMeta
	}
	return retryOnConflict(func() error {
		var existingObj rbacv1.RoleBinding
		if err := rclient.Get(ctx, nsn, &existingObj); err != nil {
			if k8serrors.IsNotFound(err) {
				logger.WithContext(ctx).Info(fmt.Sprintf("creating new RoleBinding=%s", nsn))
				return rclient.Create(ctx, newObj)
			}
			return fmt.Errorf("cannot get exist RoleBinding=%s: %w", nsn, err)
		}
		if err := collectGarbage(ctx, rclient, &existingObj); err != nil {
			return err
		}
		metaChanged, err := mergeMeta(&existingObj, newObj, prevMeta, owner)
		if err != nil {
			return err
		}
		if !metaChanged && equality.Semantic.DeepEqual(newObj.Subjects, existingObj.Subjects) &&
			equality.Semantic.DeepEqual(newObj.RoleRef, existingObj.RoleRef) {
			return nil
		}
		logger.WithContext(ctx).Info(fmt.Sprintf("updating RoleBinding=%s", nsn))
		existingObj.RoleRef = newObj.RoleRef
		existingObj.Subjects = newObj.Subjects
		return rclient.Update(ctx, &existingObj)
	})
}

// Role reconciles role object
func Role(ctx context.Context, rclient client.Client, newObj, prevObj *rbacv1.Role, owner *metav1.OwnerReference) error {
	nsn := types.NamespacedName{Name: newObj.Name, Namespace: newObj.Namespace}
	var prevMeta *metav1.ObjectMeta
	if prevObj != nil {
		prevMeta = &prevObj.ObjectMeta
	}
	return retryOnConflict(func() error {
		var existingObj rbacv1.Role
		if err := rclient.Get(ctx, nsn, &existingObj); err != nil {
			if k8serrors.IsNotFound(err) {
				logger.WithContext(ctx).Info(fmt.Sprintf("creating new Role=%s", nsn))
				return rclient.Create(ctx, newObj)
			}
			return fmt.Errorf("cannot get exist Role=%s: %w", nsn, err)
		}
		if err := collectGarbage(ctx, rclient, &existingObj); err != nil {
			return err
		}
		metaChanged, err := mergeMeta(&existingObj, newObj, prevMeta, owner)
		if err != nil {
			return err
		}
		if !metaChanged && equality.Semantic.DeepEqual(newObj.Rules, existingObj.Rules) {
			return nil
		}
		logger.WithContext(ctx).Info(fmt.Sprintf("updating Role=%s", nsn))
		existingObj.Rules = newObj.Rules
		return rclient.Update(ctx, &existingObj)
	})
}

// ClusterRoleBinding reconciles cluster role binding object
func ClusterRoleBinding(ctx context.Context, rclient client.Client, newObj, prevObj *rbacv1.ClusterRoleBinding, owner *metav1.OwnerReference) error {
	nsn := types.NamespacedName{Name: newObj.Name, Namespace: newObj.Namespace}
	var prevMeta *metav1.ObjectMeta
	if prevObj != nil {
		prevMeta = &prevObj.ObjectMeta
	}
	return retryOnConflict(func() error {
		var existingObj rbacv1.ClusterRoleBinding
		if err := rclient.Get(ctx, nsn, &existingObj); err != nil {
			if k8serrors.IsNotFound(err) {
				logger.WithContext(ctx).Info(fmt.Sprintf("creating new ClusterRoleBinding=%s", nsn))
				return rclient.Create(ctx, newObj)
			}
			return fmt.Errorf("cannot get ClusterRoleBinding=%s: %w", nsn, err)
		}
		if err := collectGarbage(ctx, rclient, &existingObj); err != nil {
			return err
		}
		metaChanged, err := mergeMeta(&existingObj, newObj, prevMeta, owner)
		if err != nil {
			return err
		}
		if !metaChanged && equality.Semantic.DeepEqual(newObj.Subjects, existingObj.Subjects) &&
			equality.Semantic.DeepEqual(newObj.RoleRef, existingObj.RoleRef) {
			return nil
		}
		logger.WithContext(ctx).Info(fmt.Sprintf("updating ClusterRoleBinding=%s", nsn))
		existingObj.RoleRef = newObj.RoleRef
		existingObj.Subjects = newObj.Subjects
		return rclient.Update(ctx, &existingObj)
	})
}

// ClusterRole reconciles cluster role object
func ClusterRole(ctx context.Context, rclient client.Client, newObj, prevObj *rbacv1.ClusterRole, owner *metav1.OwnerReference) error {
	nsn := types.NamespacedName{Name: newObj.Name, Namespace: newObj.Namespace}
	var prevMeta *metav1.ObjectMeta
	if prevObj != nil {
		prevMeta = &prevObj.ObjectMeta
	}
	return retryOnConflict(func() error {
		var existingObj rbacv1.ClusterRole
		if err := rclient.Get(ctx, nsn, &existingObj); err != nil {
			if k8serrors.IsNotFound(err) {
				logger.WithContext(ctx).Info(fmt.Sprintf("creating new ClusterRole=%s", nsn))
				return rclient.Create(ctx, newObj)
			}
			return fmt.Errorf("cannot get exist ClusterRole=%s: %w", nsn, err)
		}
		if err := collectGarbage(ctx, rclient, &existingObj); err != nil {
			return err
		}
		metaChanged, err := mergeMeta(&existingObj, newObj, prevMeta, owner)
		if err != nil {
			return err
		}
		if !metaChanged && equality.Semantic.DeepEqual(newObj.Rules, existingObj.Rules) {
			return nil
		}
		logger.WithContext(ctx).Info(fmt.Sprintf("updating ClusterRole=%s", nsn))
		existingObj.Rules = newObj.Rules
		return rclient.Update(ctx, &existingObj)
	})
}
