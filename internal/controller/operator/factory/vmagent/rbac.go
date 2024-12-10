package vmagent

import (
	"context"
	"fmt"

	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
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
func createVMAgentK8sAPIAccess(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMAgent, clusterWide bool) error {

	if err := migrateRBAC(ctx, rclient, cr, clusterWide); err != nil {
		return fmt.Errorf("cannot perform RBAC migration: %w", err)
	}
	if clusterWide {
		if err := ensureVMAgentCRExist(ctx, rclient, cr, prevCR); err != nil {
			return fmt.Errorf("cannot ensure state of vmagent's cluster role: %w", err)
		}
		if err := ensureCRBExist(ctx, rclient, cr, prevCR); err != nil {
			return fmt.Errorf("cannot ensure state of vmagent's cluster role binding: %w", err)
		}
		return nil
	}

	if err := ensureRoleExist(ctx, rclient, cr, prevCR); err != nil {
		return fmt.Errorf("cannot ensure state of vmagent's cluster role: %w", err)
	}
	if err := ensureVMAgentRBExist(ctx, rclient, cr, prevCR); err != nil {
		return fmt.Errorf("cannot ensure state of vmagent's role binding: %w", err)
	}

	return nil
}

func ensureVMAgentCRExist(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMAgent) error {
	var prevClusterRole *rbacv1.ClusterRole
	if prevCR != nil {
		prevClusterRole = buildClusterRole(prevCR)
	}
	return reconcile.ClusterRole(ctx, rclient, buildClusterRole(cr), prevClusterRole)
}

func ensureCRBExist(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMAgent) error {
	var prevCRB *rbacv1.ClusterRoleBinding
	if prevCR != nil {
		prevCRB = buildClusterRoleBinding(prevCR)
	}
	return reconcile.ClusterRoleBinding(ctx, rclient, buildClusterRoleBinding(cr), prevCRB)
}

// migrateRBAC deletes incorrectly formatted resource names
// see https://github.com/VictoriaMetrics/operator/issues/891
// and https://github.com/VictoriaMetrics/operator/pull/1176
func migrateRBAC(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAgent, clusterWide bool) error {
	const prevNamingPrefix = "monitoring:vmagent-cluster-access-"
	prevVersionName := prevNamingPrefix + cr.Name
	currentVersionName := cr.GetClusterRoleName()

	// explicitly set namespace via ObjetMeta for unit tests
	toMigrateObjects := []client.Object{
		&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Namespace: cr.Namespace}},
		&rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Namespace: cr.Namespace}},
	}
	if !clusterWide {
		toMigrateObjects = []client.Object{
			&rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Namespace: cr.Namespace}},
			&rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Namespace: cr.Namespace}},
		}
	}

	for _, obj := range toMigrateObjects {
		if err := rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: currentVersionName}, obj); err != nil {
			if !errors.IsNotFound(err) {
				return fmt.Errorf("cannot get object: %w", err)
			}
			// update name with prev version formatting
			obj.SetName(prevVersionName)
			if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, obj); err != nil {
				return fmt.Errorf("cannot safe delete obj : %w", err)
			}
		}
	}

	return nil
}

func buildClusterRoleBinding(cr *vmv1beta1.VMAgent) *rbacv1.ClusterRoleBinding {
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

func buildClusterRole(cr *vmv1beta1.VMAgent) *rbacv1.ClusterRole {
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

func ensureRoleExist(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMAgent) error {
	nr := buildNamespacedRole(cr)
	var prevRole *rbacv1.Role
	if prevCR != nil {
		prevRole = buildNamespacedRole(prevCR)
	}
	return reconcile.Role(ctx, rclient, nr, prevRole)
}

func ensureVMAgentRBExist(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMAgent) error {
	rb := buildNamespacedRoleBinding(cr)
	var prevRB *rbacv1.RoleBinding
	if prevCR != nil {
		prevRB = buildNamespacedRoleBinding(prevCR)
	}
	return reconcile.RoleBinding(ctx, rclient, rb, prevRB)
}

func buildNamespacedRole(cr *vmv1beta1.VMAgent) *rbacv1.Role {
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

func buildNamespacedRoleBinding(cr *vmv1beta1.VMAgent) *rbacv1.RoleBinding {
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
