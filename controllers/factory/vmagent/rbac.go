package vmagent

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"

	"k8s.io/apimachinery/pkg/types"

	v1beta12 "github.com/VictoriaMetrics/operator/api/v1beta1"
	v12 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CreateVMAgentClusterAccess - create cluster access for vmagent
// with clusterrole and clusterrolebinding.
func CreateVMAgentClusterAccess(ctx context.Context, cr *v1beta12.VMAgent, rclient client.Client) error {
	if err := ensureVMAgentCRExist(ctx, cr, rclient); err != nil {
		return fmt.Errorf("cannot ensure state of vmagent's cluster role: %w", err)
	}
	if err := ensureVMAgentCRBExist(ctx, cr, rclient); err != nil {
		return fmt.Errorf("cannot ensure state of vmagent's cluster role binding: %w", err)
	}

	return nil
}

func ensureVMAgentCRExist(ctx context.Context, cr *v1beta12.VMAgent, rclient client.Client) error {
	clusterRole := buildVMAgentClusterRole(cr)
	var existsClusterRole v12.ClusterRole

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
	var existsClusterRoleBinding v12.ClusterRoleBinding

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

func buildVMAgentClusterRoleBinding(cr *v1beta12.VMAgent) *v12.ClusterRoleBinding {
	return &v12.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.GetClusterRoleName(),
			Namespace:       cr.GetNamespace(),
			Labels:          cr.AllLabels(),
			Annotations:     cr.AnnotationsFiltered(),
			Finalizers:      []string{v1beta12.FinalizerName},
			OwnerReferences: cr.AsCRDOwner(),
		},
		Subjects: []v12.Subject{
			{
				Kind:      v12.ServiceAccountKind,
				Name:      cr.GetServiceAccountName(),
				Namespace: cr.GetNamespace(),
			},
		},
		RoleRef: v12.RoleRef{
			APIGroup: v12.GroupName,
			Name:     cr.GetClusterRoleName(),
			Kind:     "ClusterRole",
		},
	}
}

func buildVMAgentClusterRole(cr *v1beta12.VMAgent) *v12.ClusterRole {
	return &v12.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.GetClusterRoleName(),
			Namespace:       cr.GetNamespace(),
			Labels:          cr.AllLabels(),
			Annotations:     cr.AnnotationsFiltered(),
			Finalizers:      []string{v1beta12.FinalizerName},
			OwnerReferences: cr.AsCRDOwner(),
		},
		Rules: []v12.PolicyRule{
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
					"endpointslices",
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
		},
	}
}
