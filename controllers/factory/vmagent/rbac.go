package vmagent

import (
	"context"
	"fmt"

	k8stools "github.com/VictoriaMetrics/operator/controllers/factory/k8stools"

	v1beta12 "github.com/VictoriaMetrics/operator/api/v1beta1"
	v12 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
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
	err := k8stools.ListClusterWideObjects(ctx, rclient, &v12.ClusterRoleList{}, func(r runtime.Object) {
		items := r.(*v12.ClusterRoleList)
		for _, i := range items.Items {
			if i.Name == clusterRole.Name {
				existsClusterRole = i
				return
			}
		}
	})
	if err != nil {
		return err
	}
	if existsClusterRole.Name == "" {
		return rclient.Create(ctx, clusterRole)
	}

	existsClusterRole.Labels = labels.Merge(existsClusterRole.Labels, clusterRole.Labels)
	existsClusterRole.Annotations = labels.Merge(existsClusterRole.Annotations, clusterRole.Labels)
	return rclient.Update(ctx, &existsClusterRole)
}

func ensureVMAgentCRBExist(ctx context.Context, cr *v1beta12.VMAgent, rclient client.Client) error {
	clusterRoleBinding := buildVMAgentClusterRoleBinding(cr)
	var existsClusterRoleBinding v12.ClusterRoleBinding
	err := k8stools.ListClusterWideObjects(ctx, rclient, &v12.ClusterRoleBindingList{}, func(r runtime.Object) {
		items := r.(*v12.ClusterRoleBindingList)
		for _, i := range items.Items {
			if i.Name == clusterRoleBinding.Name {
				existsClusterRoleBinding = i
				return
			}
		}
	})
	if err != nil {
		return err
	}
	if existsClusterRoleBinding.Name == "" {
		return rclient.Create(ctx, clusterRoleBinding)
	}

	existsClusterRoleBinding.Labels = labels.Merge(existsClusterRoleBinding.Labels, clusterRoleBinding.Labels)
	existsClusterRoleBinding.Annotations = labels.Merge(existsClusterRoleBinding.Annotations, clusterRoleBinding.Labels)
	return rclient.Update(ctx, clusterRoleBinding)
}

func buildVMAgentClusterRoleBinding(cr *v1beta12.VMAgent) *v12.ClusterRoleBinding {
	return &v12.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.GetClusterRoleName(),
			Namespace:       cr.GetNamespace(),
			Labels:          cr.Labels(),
			Annotations:     cr.Annotations(),
			OwnerReferences: cr.AsOwner(),
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
			Labels:          cr.Labels(),
			Annotations:     cr.Annotations(),
			OwnerReferences: cr.AsOwner(),
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
					"nodes/proxy",
					"services",
					"endpoints",
					"pods",
					"endpointslices",
					"configmaps",
					"ingresses",
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
				NonResourceURLs: []string{"/metrics"},
				Verbs:           []string{"get", "list", "watch"},
			},
		},
	}
}
