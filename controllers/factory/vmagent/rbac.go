package vmagent

import (
	"context"
	"fmt"

	v1beta12 "github.com/VictoriaMetrics/operator/api/v1beta1"
	rbacV1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	singleNSPolicyRules = []rbacV1.PolicyRule{
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
	clusterWidePolicyRules = []rbacV1.PolicyRule{
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
func createVMAgentK8sAPIAccess(ctx context.Context, cr *v1beta12.VMAgent, rclient client.Client, clusterWide bool) error {
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

func ensureVMAgentCRExist(ctx context.Context, cr *v1beta12.VMAgent, rclient client.Client) error {
	clusterRole := buildVMAgentClusterRole(cr)
	var existsClusterRole rbacV1.ClusterRole

	if err := rclient.Get(ctx, types.NamespacedName{Name: clusterRole.Name, Namespace: cr.Namespace}, &existsClusterRole); err != nil {
		if errors.IsNotFound(err) {
			return rclient.Create(ctx, clusterRole)
		}
		return fmt.Errorf("cannot get exist cluster role for vmagent: %w", err)
	}

	existsClusterRole.OwnerReferences = clusterRole.OwnerReferences
	existsClusterRole.Labels = clusterRole.Labels
	existsClusterRole.Annotations = labels.Merge(existsClusterRole.Annotations, clusterRole.Annotations)
	existsClusterRole.Rules = clusterRole.Rules
	v1beta12.MergeFinalizers(&existsClusterRole, v1beta12.FinalizerName)
	return rclient.Update(ctx, &existsClusterRole)
}

func ensureVMAgentCRBExist(ctx context.Context, cr *v1beta12.VMAgent, rclient client.Client) error {
	clusterRoleBinding := buildVMAgentClusterRoleBinding(cr)
	var existsClusterRoleBinding rbacV1.ClusterRoleBinding

	if err := rclient.Get(ctx, types.NamespacedName{Name: clusterRoleBinding.Name, Namespace: cr.Namespace}, &existsClusterRoleBinding); err != nil {
		if errors.IsNotFound(err) {
			return rclient.Create(ctx, clusterRoleBinding)
		}
		return fmt.Errorf("cannot get clusterRoleBinding for vmagent: %w", err)
	}

	existsClusterRoleBinding.OwnerReferences = clusterRoleBinding.OwnerReferences
	existsClusterRoleBinding.Labels = clusterRoleBinding.Labels
	existsClusterRoleBinding.Annotations = labels.Merge(existsClusterRoleBinding.Annotations, clusterRoleBinding.Annotations)
	existsClusterRoleBinding.Subjects = clusterRoleBinding.Subjects
	existsClusterRoleBinding.RoleRef = clusterRoleBinding.RoleRef
	v1beta12.MergeFinalizers(&existsClusterRoleBinding, v1beta12.FinalizerName)
	return rclient.Update(ctx, &existsClusterRoleBinding)
}

func buildVMAgentClusterRoleBinding(cr *v1beta12.VMAgent) *rbacV1.ClusterRoleBinding {
	return &rbacV1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.GetClusterRoleName(),
			Namespace:       cr.GetNamespace(),
			Labels:          cr.AllLabels(),
			Annotations:     cr.AnnotationsFiltered(),
			Finalizers:      []string{v1beta12.FinalizerName},
			OwnerReferences: cr.AsCRDOwner(),
		},
		Subjects: []rbacV1.Subject{
			{
				Kind:      rbacV1.ServiceAccountKind,
				Name:      cr.GetServiceAccountName(),
				Namespace: cr.GetNamespace(),
			},
		},
		RoleRef: rbacV1.RoleRef{
			APIGroup: rbacV1.GroupName,
			Name:     cr.GetClusterRoleName(),
			Kind:     "ClusterRole",
		},
	}
}

func buildVMAgentClusterRole(cr *v1beta12.VMAgent) *rbacV1.ClusterRole {
	return &rbacV1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.GetClusterRoleName(),
			Namespace:       cr.GetNamespace(),
			Labels:          cr.AllLabels(),
			Annotations:     cr.AnnotationsFiltered(),
			Finalizers:      []string{v1beta12.FinalizerName},
			OwnerReferences: cr.AsCRDOwner(),
		},
		Rules: clusterWidePolicyRules,
	}
}

func ensureVMAgentRExist(ctx context.Context, cr *v1beta12.VMAgent, rclient client.Client) error {
	nr := buildVMAgentNamespaceRole(cr)
	var existsClusterRole rbacV1.Role

	if err := rclient.Get(ctx, types.NamespacedName{Name: nr.Name, Namespace: cr.Namespace}, &existsClusterRole); err != nil {
		if errors.IsNotFound(err) {
			return rclient.Create(ctx, nr)
		}
		return fmt.Errorf("cannot get exist cluster role for vmagent: %w", err)
	}

	existsClusterRole.OwnerReferences = nr.OwnerReferences
	existsClusterRole.Labels = nr.Labels
	existsClusterRole.Annotations = labels.Merge(existsClusterRole.Annotations, nr.Annotations)
	existsClusterRole.Rules = nr.Rules
	v1beta12.MergeFinalizers(&existsClusterRole, v1beta12.FinalizerName)
	return rclient.Update(ctx, &existsClusterRole)
}

func ensureVMAgentRBExist(ctx context.Context, cr *v1beta12.VMAgent, rclient client.Client) error {
	rb := buildVMAgentNamespaceRoleBinding(cr)
	var existsRB rbacV1.RoleBinding

	if err := rclient.Get(ctx, types.NamespacedName{Name: rb.Name, Namespace: cr.Namespace}, &existsRB); err != nil {
		if errors.IsNotFound(err) {
			return rclient.Create(ctx, rb)
		}
		return fmt.Errorf("cannot get rb for vmagent: %w", err)
	}

	existsRB.OwnerReferences = rb.OwnerReferences
	existsRB.Labels = rb.Labels
	existsRB.Annotations = labels.Merge(existsRB.Annotations, rb.Annotations)
	existsRB.Subjects = rb.Subjects
	existsRB.RoleRef = rb.RoleRef
	v1beta12.MergeFinalizers(&existsRB, v1beta12.FinalizerName)
	return rclient.Update(ctx, &existsRB)
}

func buildVMAgentNamespaceRole(cr *v1beta12.VMAgent) *rbacV1.Role {
	return &rbacV1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.GetClusterRoleName(),
			Namespace:       cr.GetNamespace(),
			Labels:          cr.AllLabels(),
			Annotations:     cr.AnnotationsFiltered(),
			Finalizers:      []string{v1beta12.FinalizerName},
			OwnerReferences: cr.AsCRDOwner(),
		},
		Rules: singleNSPolicyRules,
	}
}

func buildVMAgentNamespaceRoleBinding(cr *v1beta12.VMAgent) *rbacV1.RoleBinding {
	return &rbacV1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.GetClusterRoleName(),
			Namespace:       cr.GetNamespace(),
			Labels:          cr.AllLabels(),
			Annotations:     cr.AnnotationsFiltered(),
			Finalizers:      []string{v1beta12.FinalizerName},
			OwnerReferences: cr.AsCRDOwner(),
		},
		Subjects: []rbacV1.Subject{
			{
				Kind:      rbacV1.ServiceAccountKind,
				Name:      cr.GetServiceAccountName(),
				Namespace: cr.GetNamespace(),
			},
		},
		RoleRef: rbacV1.RoleRef{
			APIGroup: rbacV1.GroupName,
			Name:     cr.GetClusterRoleName(),
			Kind:     "Role",
		},
	}
}
