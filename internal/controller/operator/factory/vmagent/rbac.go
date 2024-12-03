package vmagent

import (
	"context"
	"fmt"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"

	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	singleNSPolicyRules = []rbacv1.PolicyRule{
		{
			APIGroups: []string{"discovery.k8s.io"},
			Verbs: []string{
				"get",
				"list",
				"watch",
			},
			Resources: []string{
				"endpointslices",
			},
		},
		{
			APIGroups: []string{""},
			Verbs: []string{
				"get",
				"list",
				"watch",
			},
			Resources: []string{
				"services",
				"endpoints",
				"pods",
				"secrets",
				"configmaps",
			},
		},
		{
			APIGroups: []string{"networking.k8s.io", "extensions"},
			Verbs: []string{
				"get",
				"list",
				"watch",
			},
			Resources: []string{
				"ingresses",
			},
		},
	}
	clusterWidePolicyRules = []rbacv1.PolicyRule{
		{
			APIGroups: []string{"discovery.k8s.io"},
			Verbs: []string{
				"get",
				"list",
				"watch",
			},
			Resources: []string{
				"endpointslices",
			},
		},
		{
			APIGroups: []string{""},
			Verbs: []string{
				"get",
				"list",
				"watch",
			},
			Resources: []string{
				"nodes",
				"nodes/metrics",
				"nodes/proxy",
				"services",
				"endpoints",
				"pods",
				"configmaps",
				"namespaces",
				"secrets",
			},
		},
		{
			APIGroups: []string{"networking.k8s.io", "extensions"},
			Verbs: []string{
				"get",
				"list",
				"watch",
			},
			Resources: []string{
				"ingresses",
			},
		},
		{
			NonResourceURLs: []string{"/metrics", "/metrics/resources"},
			Verbs:           []string{"get", "list", "watch"},
		},
		{
			APIGroups: []string{"route.openshift.io", "image.openshift.io"},
			Verbs: []string{
				"get",
			},
			Resources: []string{
				"routers/metrics", "registry/metrics",
			},
		},
	}
)

// createVMAgentK8sAPIAccess - creates RBAC access rules for vmagent
func createVMAgentK8sAPIAccess(ctx context.Context, cr *vmv1beta1.VMAgent, rclient client.Client, clusterWide bool) error {
	if clusterWide {
		if err := ensureVMAgentCRExist(ctx, cr, rclient); err != nil {
			return fmt.Errorf("cannot ensure state of vmagent's cluster role: %w", err)
		}
		if err := ensureVMAgentCRBExist(ctx, cr, rclient); err != nil {
			return fmt.Errorf("cannot ensure state of vmagent's cluster role binding: %w", err)
		}
		return nil
	}

	if err := ensureVMAgentRExist(ctx, cr, rclient); err != nil {
		return fmt.Errorf("cannot ensure state of vmagent's cluster role: %w", err)
	}
	if err := ensureVMAgentRBExist(ctx, cr, rclient); err != nil {
		return fmt.Errorf("cannot ensure state of vmagent's role binding: %w", err)
	}

	return nil
}

func ensureVMAgentCRExist(ctx context.Context, cr *vmv1beta1.VMAgent, rclient client.Client) error {
	clusterRole := buildVMAgentClusterRole(cr)
	var existsClusterRole rbacv1.ClusterRole

	if err := rclient.Get(ctx, types.NamespacedName{Name: clusterRole.Name}, &existsClusterRole); err != nil {
		if errors.IsNotFound(err) {
			// check for possible legacy ClusterRole and clean it, see https://github.com/VictoriaMetrics/operator/pull/1176
			var orphanedClusterRole rbacv1.ClusterRole
			err = rclient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf("monitoring:vmagent-cluster-access-%s", cr.Name)}, &orphanedClusterRole)
			if err != nil {
				logger.WithContext(ctx).Error(err, "failed to check legacy ClusterRole", "vmagent", cr.Name, "namespace", cr.Namespace)
			}
			if orphanedClusterRole.Name != "" {
				finalize.RemoveFinalizer(ctx, rclient, &orphanedClusterRole)
				err = rclient.Delete(ctx, &orphanedClusterRole)
				if err != nil {
					logger.WithContext(ctx).Error(err, "failed to cleanup legacy ClusterRole", "vmagent", cr.Name, "namespace", cr.Namespace)
				}
			}

			return rclient.Create(ctx, clusterRole)
		}
		return fmt.Errorf("cannot get exist cluster role for vmagent: %w", err)
	}
	if err := finalize.FreeIfNeeded(ctx, rclient, &existsClusterRole); err != nil {
		return err
	}
	// TODO compare OwnerReferences
	// fast path
	if equality.Semantic.DeepEqual(clusterRole.Rules, existsClusterRole.Rules) &&
		equality.Semantic.DeepEqual(clusterRole.Labels, existsClusterRole.Labels) &&
		equality.Semantic.DeepEqual(clusterRole.Annotations, existsClusterRole.Annotations) {
		return nil
	}
	logger.WithContext(ctx).Info("updating VMAgent ClusterRole")

	existsClusterRole.OwnerReferences = clusterRole.OwnerReferences
	existsClusterRole.Labels = clusterRole.Labels
	existsClusterRole.Annotations = labels.Merge(existsClusterRole.Annotations, clusterRole.Annotations)
	existsClusterRole.Rules = clusterRole.Rules
	vmv1beta1.AddFinalizer(&existsClusterRole, &existsClusterRole)
	return rclient.Update(ctx, &existsClusterRole)
}

func ensureVMAgentCRBExist(ctx context.Context, cr *vmv1beta1.VMAgent, rclient client.Client) error {
	clusterRoleBinding := buildVMAgentClusterRoleBinding(cr)
	var existsClusterRoleBinding rbacv1.ClusterRoleBinding

	if err := rclient.Get(ctx, types.NamespacedName{Name: clusterRoleBinding.Name}, &existsClusterRoleBinding); err != nil {
		if errors.IsNotFound(err) {
			// check for possible legacy ClusterRoleBinding and clean it, see https://github.com/VictoriaMetrics/operator/pull/1176
			var orphanedClusterRoleBinding rbacv1.ClusterRoleBinding
			err = rclient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf("monitoring:vmagent-cluster-access-%s", cr.Name)}, &orphanedClusterRoleBinding)
			if err != nil {
				logger.WithContext(ctx).Error(err, "failed to check legacy ClusterRoleBinding", "vmagent", cr.Name, "namespace", cr.Namespace)
			}
			if orphanedClusterRoleBinding.Name != "" {
				finalize.RemoveFinalizer(ctx, rclient, &orphanedClusterRoleBinding)
				err = rclient.Delete(ctx, &orphanedClusterRoleBinding)
				if err != nil {
					logger.WithContext(ctx).Error(err, "failed to cleanup legacy ClusterRoleBinding", "vmagent", cr.Name, "namespace", cr.Namespace)
				}
			}

			return rclient.Create(ctx, clusterRoleBinding)
		}
		return fmt.Errorf("cannot get clusterRoleBinding for vmagent: %w", err)
	}
	if err := finalize.FreeIfNeeded(ctx, rclient, &existsClusterRoleBinding); err != nil {
		return err
	}
	// TODO compare OwnerReferences

	// fast path
	if equality.Semantic.DeepEqual(clusterRoleBinding.Subjects, existsClusterRoleBinding.Subjects) &&
		equality.Semantic.DeepEqual(clusterRoleBinding.RoleRef, existsClusterRoleBinding.RoleRef) &&
		equality.Semantic.DeepEqual(clusterRoleBinding.Labels, existsClusterRoleBinding.Labels) &&
		equality.Semantic.DeepEqual(clusterRoleBinding.Annotations, existsClusterRoleBinding.Annotations) {
		return nil
	}
	logger.WithContext(ctx).Info("updating VMAgent ClusterRoleBinding")

	existsClusterRoleBinding.OwnerReferences = clusterRoleBinding.OwnerReferences
	existsClusterRoleBinding.Labels = clusterRoleBinding.Labels
	existsClusterRoleBinding.Annotations = labels.Merge(existsClusterRoleBinding.Annotations, clusterRoleBinding.Annotations)
	existsClusterRoleBinding.Subjects = clusterRoleBinding.Subjects
	existsClusterRoleBinding.RoleRef = clusterRoleBinding.RoleRef
	vmv1beta1.AddFinalizer(&existsClusterRoleBinding, &existsClusterRoleBinding)
	return rclient.Update(ctx, &existsClusterRoleBinding)
}

func buildVMAgentClusterRoleBinding(cr *vmv1beta1.VMAgent) *rbacv1.ClusterRoleBinding {
	return &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:        cr.GetClusterRoleName(),
			Namespace:   cr.GetNamespace(),
			Labels:      cr.AllLabels(),
			Annotations: cr.AnnotationsFiltered(),
			Finalizers:  []string{vmv1beta1.FinalizerName},
			// Kubernetes does not allow namespace-scoped resources to own cluster-scoped resources,
			// use crd instead
			OwnerReferences: cr.AsCRDOwner(),
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      rbacv1.ServiceAccountKind,
				Name:      cr.GetServiceAccountName(),
				Namespace: cr.GetNamespace(),
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Name:     cr.GetClusterRoleName(),
			Kind:     "ClusterRole",
		},
	}
}

func buildVMAgentClusterRole(cr *vmv1beta1.VMAgent) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:        cr.GetClusterRoleName(),
			Namespace:   cr.GetNamespace(),
			Labels:      cr.AllLabels(),
			Annotations: cr.AnnotationsFiltered(),
			Finalizers:  []string{vmv1beta1.FinalizerName},
			// Kubernetes does not allow namespace-scoped resources to own cluster-scoped resources,
			// use crd instead
			OwnerReferences: cr.AsCRDOwner(),
		},
		Rules: clusterWidePolicyRules,
	}
}

func ensureVMAgentRExist(ctx context.Context, cr *vmv1beta1.VMAgent, rclient client.Client) error {
	nr := buildVMAgentNamespaceRole(cr)
	return reconcile.Role(ctx, rclient, nr)
}

func ensureVMAgentRBExist(ctx context.Context, cr *vmv1beta1.VMAgent, rclient client.Client) error {
	rb := buildVMAgentNamespaceRoleBinding(cr)
	return reconcile.RoleBinding(ctx, rclient, rb)
}

func buildVMAgentNamespaceRole(cr *vmv1beta1.VMAgent) *rbacv1.Role {
	return &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.GetClusterRoleName(),
			Namespace:       cr.GetNamespace(),
			Labels:          cr.AllLabels(),
			Annotations:     cr.AnnotationsFiltered(),
			Finalizers:      []string{vmv1beta1.FinalizerName},
			OwnerReferences: cr.AsOwner(),
		},
		Rules: singleNSPolicyRules,
	}
}

func buildVMAgentNamespaceRoleBinding(cr *vmv1beta1.VMAgent) *rbacv1.RoleBinding {
	return &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.GetClusterRoleName(),
			Namespace:       cr.GetNamespace(),
			Labels:          cr.AllLabels(),
			Annotations:     cr.AnnotationsFiltered(),
			Finalizers:      []string{vmv1beta1.FinalizerName},
			OwnerReferences: cr.AsOwner(),
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      rbacv1.ServiceAccountKind,
				Name:      cr.GetServiceAccountName(),
				Namespace: cr.GetNamespace(),
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Name:     cr.GetClusterRoleName(),
			Kind:     "Role",
		},
	}
}
