package psp

import (
	"context"
	"fmt"

	k8stools "github.com/VictoriaMetrics/operator/controllers/factory/k8stools"

	"k8s.io/apimachinery/pkg/runtime"

	v12 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/labels"

	"k8s.io/utils/pointer"

	"k8s.io/api/policy/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type CRDObject interface {
	AsOwner() []metav1.OwnerReference
	Annotations() map[string]string
	Labels() map[string]string
	PrefixedName() string
	GetServiceAccountName() string
	GetPSPName() string
	GetNamespace() string
}

// CreateOrUpdateServiceAccountWithPSP - creates psp for api object.
// ensure that ServiceAccount exists,
// PodSecurityPolicy exists, we only update it, if its our PSP.
// ClusterRole exists,
// ClusterRoleBinding exists.
func CreateOrUpdateServiceAccountWithPSP(ctx context.Context, cr CRDObject, rclient client.Client) error {

	if err := ensurePSPExists(ctx, cr, rclient); err != nil {
		return fmt.Errorf("failed check policy: %w", err)
	}
	if err := ensureClusterRoleExists(ctx, cr, rclient); err != nil {
		return fmt.Errorf("failed check clusterrole: %w", err)
	}
	if err := ensureClusterRoleBindingExists(ctx, cr, rclient); err != nil {
		return fmt.Errorf("failed check clusterrolebinding: %w", err)
	}
	return nil
}

func CreateServiceAccountForCRD(ctx context.Context, cr CRDObject, rclient client.Client) error {
	newSA := buildSA(cr)
	var existSA v1.ServiceAccount
	if err := rclient.Get(ctx, types.NamespacedName{Name: cr.GetServiceAccountName(), Namespace: cr.GetNamespace()}, &existSA); err != nil {
		if errors.IsNotFound(err) {
			return rclient.Create(ctx, newSA)
		}
		return fmt.Errorf("cannot get ServiceAccount for given CRD Object=%q, err=%w", cr.PrefixedName(), err)
	}
	return nil
}

func ensurePSPExists(ctx context.Context, cr CRDObject, rclient client.Client) error {
	defaultPSP := BuildPSP(cr)
	var existPSP v1beta1.PodSecurityPolicy
	err := k8stools.ListClusterWideObjects(ctx, rclient, &v1beta1.PodSecurityPolicyList{}, func(r runtime.Object) {
		items := r.(*v1beta1.PodSecurityPolicyList)
		for _, i := range items.Items {
			if i.Name == defaultPSP.Name {
				existPSP = i
				return
			}
		}
	})
	if err != nil {
		return err
	}
	if existPSP.Name == "" {
		return rclient.Create(ctx, defaultPSP)
	}
	// check if its ours, if so, update it
	if cr.GetPSPName() != cr.PrefixedName() {
		return nil
	}
	existPSP.Labels = labels.Merge(existPSP.Labels, defaultPSP.Labels)
	existPSP.Annotations = labels.Merge(existPSP.Annotations, defaultPSP.Labels)
	return rclient.Update(ctx, &existPSP)
}

func ensureClusterRoleExists(ctx context.Context, cr CRDObject, rclient client.Client) error {
	clusterRole := buildClusterRoleForPSP(cr)
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
	existsClusterRole.Annotations = labels.Merge(clusterRole.Annotations, existsClusterRole.Annotations)
	return rclient.Update(ctx, &existsClusterRole)
}

func ensureClusterRoleBindingExists(ctx context.Context, cr CRDObject, rclient client.Client) error {
	clusterRoleBinding := buildClusterRoleBinding(cr)
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
	existsClusterRoleBinding.Annotations = labels.Merge(clusterRoleBinding.Annotations, existsClusterRoleBinding.Annotations)
	return rclient.Update(ctx, clusterRoleBinding)
}

func buildSA(cr CRDObject) *v1.ServiceAccount {
	return &v1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.GetServiceAccountName(),
			Namespace:       cr.GetNamespace(),
			Labels:          cr.Labels(),
			Annotations:     cr.Annotations(),
			OwnerReferences: cr.AsOwner(),
		},
	}
}

func buildClusterRoleForPSP(cr CRDObject) *v12.ClusterRole {
	return &v12.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   cr.GetNamespace(),
			Name:        cr.PrefixedName(),
			Labels:      cr.Labels(),
			Annotations: cr.Annotations(),
		},
		Rules: []v12.PolicyRule{
			{
				Resources:     []string{"podsecuritypolicies"},
				Verbs:         []string{"use"},
				APIGroups:     []string{"policy"},
				ResourceNames: []string{cr.GetPSPName()},
			},
		},
	}
}

func buildClusterRoleBinding(cr CRDObject) *v12.ClusterRoleBinding {
	return &v12.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:        cr.PrefixedName(),
			Namespace:   cr.GetNamespace(),
			Labels:      cr.Labels(),
			Annotations: cr.Annotations(),
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
			Name:     cr.PrefixedName(),
			Kind:     "ClusterRole",
		},
	}
}

func BuildPSP(cr CRDObject) *v1beta1.PodSecurityPolicy {
	return &v1beta1.PodSecurityPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:        cr.GetPSPName(),
			Namespace:   cr.GetNamespace(),
			Labels:      cr.Labels(),
			Annotations: cr.Annotations(),
		},
		Spec: v1beta1.PodSecurityPolicySpec{
			ReadOnlyRootFilesystem: false,
			Volumes: []v1beta1.FSType{
				v1beta1.PersistentVolumeClaim,
				v1beta1.Secret,
				v1beta1.EmptyDir,
				v1beta1.ConfigMap,
				v1beta1.Projected,
				v1beta1.DownwardAPI,
				v1beta1.NFS,
			},
			AllowPrivilegeEscalation: pointer.BoolPtr(false),
			HostNetwork:              true,
			RunAsUser: v1beta1.RunAsUserStrategyOptions{
				Rule: v1beta1.RunAsUserStrategyRunAsAny,
			},
			SELinux:            v1beta1.SELinuxStrategyOptions{Rule: v1beta1.SELinuxStrategyRunAsAny},
			SupplementalGroups: v1beta1.SupplementalGroupsStrategyOptions{Rule: v1beta1.SupplementalGroupsStrategyRunAsAny},
			FSGroup:            v1beta1.FSGroupStrategyOptions{Rule: v1beta1.FSGroupStrategyRunAsAny},
			HostPID:            false,
			HostIPC:            false,
			RequiredDropCapabilities: []v1.Capability{
				"ALL",
			},
		}}
}
