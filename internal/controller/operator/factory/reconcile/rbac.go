package reconcile

import (
	"context"
	"fmt"
	"strings"

	rbacv1 "k8s.io/api/rbac/v1"
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
	removeFinalizer := true
	return retryOnConflict(func() error {
		var existingObj rbacv1.RoleBinding
		if err := rclient.Get(ctx, nsn, &existingObj); err != nil {
			if k8serrors.IsNotFound(err) {
				logger.WithContext(ctx).Info(fmt.Sprintf("creating new RoleBinding=%s", nsn.String()))
				return rclient.Create(ctx, newObj)
			}
			return fmt.Errorf("cannot get exist RoleBinding=%s: %w", nsn.String(), err)
		}
		if err := collectGarbage(ctx, rclient, &existingObj, removeFinalizer); err != nil {
			return err
		}
		metaChanged, err := mergeMeta(&existingObj, newObj, prevMeta, owner, removeFinalizer, false)
		if err != nil {
			return err
		}
		logMessageMetadata := []string{fmt.Sprintf("name=%s, is_prev_nil=%t", nsn.String(), prevObj == nil)}
		subjectsDiff := diffDeepDerivative(newObj.Subjects, existingObj.Subjects)
		needsUpdate := metaChanged || len(subjectsDiff) > 0
		logMessageMetadata = append(logMessageMetadata, fmt.Sprintf("subjects_diff=%s", subjectsDiff))
		roleRefDiff := diffDeepDerivative(newObj.RoleRef, existingObj.RoleRef)
		needsUpdate = needsUpdate || len(roleRefDiff) > 0
		logMessageMetadata = append(logMessageMetadata, fmt.Sprintf("role_ref_diff=%s", roleRefDiff))
		if !needsUpdate {
			return nil
		}
		existingObj.RoleRef = newObj.RoleRef
		existingObj.Subjects = newObj.Subjects
		logger.WithContext(ctx).Info(fmt.Sprintf("updating RoleBinding %s", strings.Join(logMessageMetadata, ", ")))
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
	removeFinalizer := true
	return retryOnConflict(func() error {
		var existingObj rbacv1.Role
		if err := rclient.Get(ctx, nsn, &existingObj); err != nil {
			if k8serrors.IsNotFound(err) {
				logger.WithContext(ctx).Info(fmt.Sprintf("creating new Role=%s", nsn.String()))
				return rclient.Create(ctx, newObj)
			}
			return fmt.Errorf("cannot get exist Role=%s: %w", nsn.String(), err)
		}
		if err := collectGarbage(ctx, rclient, &existingObj, removeFinalizer); err != nil {
			return err
		}
		metaChanged, err := mergeMeta(&existingObj, newObj, prevMeta, owner, removeFinalizer, false)
		if err != nil {
			return err
		}
		logMessageMetadata := []string{fmt.Sprintf("name=%s, is_prev_nil=%t", nsn.String(), prevObj == nil)}
		rulesDiff := diffDeepDerivative(newObj.Rules, existingObj.Rules)
		needsUpdate := metaChanged || len(rulesDiff) > 0
		logMessageMetadata = append(logMessageMetadata, fmt.Sprintf("rules_diff=%s", rulesDiff))
		if !needsUpdate {
			return nil
		}
		existingObj.Rules = newObj.Rules
		logger.WithContext(ctx).Info(fmt.Sprintf("updating Role %s", strings.Join(logMessageMetadata, ", ")))
		return rclient.Update(ctx, &existingObj)
	})
}

// ClusterRoleBinding reconciles cluster role binding object
func ClusterRoleBinding(ctx context.Context, rclient client.Client, newObj, prevObj *rbacv1.ClusterRoleBinding) error {
	nsn := types.NamespacedName{Name: newObj.Name, Namespace: newObj.Namespace}
	var prevMeta *metav1.ObjectMeta
	if prevObj != nil {
		prevMeta = &prevObj.ObjectMeta
	}
	removeFinalizer := true
	return retryOnConflict(func() error {
		var existingObj rbacv1.ClusterRoleBinding
		if err := rclient.Get(ctx, nsn, &existingObj); err != nil {
			if k8serrors.IsNotFound(err) {
				logger.WithContext(ctx).Info(fmt.Sprintf("creating new ClusterRoleBinding=%s", nsn.String()))
				return rclient.Create(ctx, newObj)
			}
			return fmt.Errorf("cannot get ClusterRoleBinding=%s: %w", nsn.String(), err)
		}
		if err := collectGarbage(ctx, rclient, &existingObj, removeFinalizer); err != nil {
			return err
		}
		metaChanged, err := mergeMeta(&existingObj, newObj, prevMeta, nil, removeFinalizer, false)
		if err != nil {
			return err
		}
		logMessageMetadata := []string{fmt.Sprintf("name=%s, is_prev_nil=%t", nsn.String(), prevObj == nil)}
		subjectsDiff := diffDeepDerivative(newObj.Subjects, existingObj.Subjects)
		needsUpdate := metaChanged || len(subjectsDiff) > 0
		logMessageMetadata = append(logMessageMetadata, fmt.Sprintf("subjects_diff=%s", subjectsDiff))
		roleRefDiff := diffDeepDerivative(newObj.RoleRef, existingObj.RoleRef)
		needsUpdate = needsUpdate || len(roleRefDiff) > 0
		logMessageMetadata = append(logMessageMetadata, fmt.Sprintf("role_ref_diff=%s", roleRefDiff))
		if !needsUpdate {
			return nil
		}
		existingObj.RoleRef = newObj.RoleRef
		existingObj.Subjects = newObj.Subjects
		logger.WithContext(ctx).Info(fmt.Sprintf("updating ClusterRoleBinding %s", strings.Join(logMessageMetadata, ", ")))
		return rclient.Update(ctx, &existingObj)
	})
}

// ClusterRole reconciles cluster role object
func ClusterRole(ctx context.Context, rclient client.Client, newObj, prevObj *rbacv1.ClusterRole) error {
	nsn := types.NamespacedName{Name: newObj.Name, Namespace: newObj.Namespace}
	var prevMeta *metav1.ObjectMeta
	if prevObj != nil {
		prevMeta = &prevObj.ObjectMeta
	}
	removeFinalizer := true
	return retryOnConflict(func() error {
		var existingObj rbacv1.ClusterRole
		if err := rclient.Get(ctx, nsn, &existingObj); err != nil {
			if k8serrors.IsNotFound(err) {
				logger.WithContext(ctx).Info(fmt.Sprintf("creating new ClusterRole=%s", nsn.String()))
				return rclient.Create(ctx, newObj)
			}
			return fmt.Errorf("cannot get exist ClusterRole=%s: %w", nsn.String(), err)
		}
		if err := collectGarbage(ctx, rclient, &existingObj, removeFinalizer); err != nil {
			return err
		}
		metaChanged, err := mergeMeta(&existingObj, newObj, prevMeta, nil, removeFinalizer, false)
		if err != nil {
			return err
		}
		logMessageMetadata := []string{fmt.Sprintf("name=%s, is_prev_nil=%t", nsn.String(), prevObj == nil)}
		rulesDiff := diffDeepDerivative(newObj.Rules, existingObj.Rules)
		needsUpdate := metaChanged || len(rulesDiff) > 0
		logMessageMetadata = append(logMessageMetadata, fmt.Sprintf("rules_diff=%s", rulesDiff))
		if !needsUpdate {
			return nil
		}
		existingObj.Rules = newObj.Rules
		logger.WithContext(ctx).Info(fmt.Sprintf("updating ClusterRole %s", strings.Join(logMessageMetadata, ", ")))
		return rclient.Update(ctx, &existingObj)
	})
}
