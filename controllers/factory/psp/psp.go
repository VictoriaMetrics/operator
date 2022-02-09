package psp

import (
	"context"
	"fmt"
	v1beta12 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/controllers/factory/k8stools"
	v1 "k8s.io/api/core/v1"
	"k8s.io/api/policy/v1beta1"
	v12 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type CRDObject interface {
	Annotations() map[string]string
	Labels() map[string]string
	PrefixedName() string
	GetServiceAccountName() string
	GetPSPName() string
	GetNSName() string
	AsOwner() []metav1.OwnerReference
	AsCRDOwner() []metav1.OwnerReference
}

// CreateOrUpdateServiceAccountWithPSP - creates psp for api object.
// ensure that ServiceAccount exists,
// PodSecurityPolicy exists, we only update it, if its our PSP.
// ClusterRole exists,
// ClusterRoleBinding exists.
func CreateOrUpdateServiceAccountWithPSP(ctx context.Context, cr CRDObject, rclient client.Client) error {

	if !k8stools.IsPSPSupported() {
		return nil
	}
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
	if err := rclient.Get(ctx, types.NamespacedName{Name: cr.GetServiceAccountName(), Namespace: cr.GetNSName()}, &existSA); err != nil {
		if errors.IsNotFound(err) {
			return rclient.Create(ctx, newSA)
		}
		return fmt.Errorf("cannot get ServiceAccount for given CRD Object=%q, err=%w", cr.PrefixedName(), err)
	}

	existSA.OwnerReferences = newSA.OwnerReferences
	existSA.Finalizers = v1beta12.MergeFinalizers(&existSA, v1beta12.FinalizerName)
	existSA.Annotations = labels.Merge(existSA.Annotations, newSA.Annotations)
	existSA.Labels = newSA.Labels
	return rclient.Update(ctx, &existSA)
}

func ensurePSPExists(ctx context.Context, cr CRDObject, rclient client.Client) error {

	// fast path.
	if cr.GetPSPName() != cr.PrefixedName() {
		return nil
	}
	newPSP := BuildPSP(cr)
	var existPSP v1beta1.PodSecurityPolicy
	if err := rclient.Get(ctx, types.NamespacedName{Namespace: cr.GetNSName(), Name: newPSP.Name}, &existPSP); err != nil {
		if errors.IsNotFound(err) {
			return rclient.Create(ctx, newPSP)
		}
		return fmt.Errorf("cannot get exists PSP :%w", err)
	}
	newPSP.Annotations = labels.Merge(existPSP.Annotations, newPSP.Annotations)
	newPSP.Finalizers = v1beta12.MergeFinalizers(&existPSP, v1beta12.FinalizerName)
	return rclient.Update(ctx, newPSP)
}

func ensureClusterRoleExists(ctx context.Context, cr CRDObject, rclient client.Client) error {
	clusterRole := buildClusterRoleForPSP(cr)
	var existsClusterRole v12.ClusterRole
	if err := rclient.Get(ctx, types.NamespacedName{Namespace: cr.GetNSName(), Name: clusterRole.Name}, &existsClusterRole); err != nil {
		if errors.IsNotFound(err) {
			return rclient.Create(ctx, clusterRole)

		}
		return fmt.Errorf("cannot get existClusterRole: %w", err)
	}

	clusterRole.Annotations = labels.Merge(existsClusterRole.Annotations, clusterRole.Annotations)
	clusterRole.Finalizers = v1beta12.MergeFinalizers(&existsClusterRole, v1beta12.FinalizerName)
	return rclient.Update(ctx, clusterRole)
}

func ensureClusterRoleBindingExists(ctx context.Context, cr CRDObject, rclient client.Client) error {
	clusterRoleBinding := buildClusterRoleBinding(cr)
	var existsClusterRoleBinding v12.ClusterRoleBinding
	if err := rclient.Get(ctx, types.NamespacedName{Namespace: cr.GetNSName(), Name: clusterRoleBinding.Name}, &existsClusterRoleBinding); err != nil {
		if errors.IsNotFound(err) {
			return rclient.Create(ctx, clusterRoleBinding)

		}
		return fmt.Errorf("cannot get clusterRoleBinding: %w", err)
	}

	clusterRoleBinding.Annotations = labels.Merge(existsClusterRoleBinding.Annotations, clusterRoleBinding.Annotations)
	clusterRoleBinding.Finalizers = v1beta12.MergeFinalizers(&existsClusterRoleBinding, v1beta12.FinalizerName)
	return rclient.Update(ctx, clusterRoleBinding)
}

func buildSA(cr CRDObject) *v1.ServiceAccount {
	return &v1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.GetServiceAccountName(),
			Namespace:       cr.GetNSName(),
			Labels:          cr.Labels(),
			Annotations:     cr.Annotations(),
			OwnerReferences: cr.AsOwner(),
			Finalizers:      []string{v1beta12.FinalizerName},
		},
	}
}

func buildClusterRoleForPSP(cr CRDObject) *v12.ClusterRole {
	return &v12.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       cr.GetNSName(),
			Name:            cr.PrefixedName(),
			Labels:          cr.Labels(),
			Annotations:     cr.Annotations(),
			Finalizers:      []string{v1beta12.FinalizerName},
			OwnerReferences: cr.AsCRDOwner(),
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
			Name:            cr.PrefixedName(),
			Namespace:       cr.GetNSName(),
			Labels:          cr.Labels(),
			Annotations:     cr.Annotations(),
			Finalizers:      []string{v1beta12.FinalizerName},
			OwnerReferences: cr.AsCRDOwner(),
		},
		Subjects: []v12.Subject{
			{
				Kind:      v12.ServiceAccountKind,
				Name:      cr.GetServiceAccountName(),
				Namespace: cr.GetNSName(),
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
			Name:            cr.GetPSPName(),
			Namespace:       cr.GetNSName(),
			Labels:          cr.Labels(),
			Annotations:     cr.Annotations(),
			Finalizers:      []string{v1beta12.FinalizerName},
			OwnerReferences: cr.AsCRDOwner(),
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
