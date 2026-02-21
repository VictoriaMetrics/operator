package vmagent

import (
	"context"
	"fmt"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

func getSingleNamespaceRules(cr *vmv1beta1.VMAgent) []rbacv1.PolicyRule {
	var rules []rbacv1.PolicyRule
	if !ptr.Deref(cr.Spec.IngestOnlyMode, false) || cr.HasAnyRelabellingConfigs() || cr.HasAnyStreamAggrRule() {
		rules = append(rules, rbacv1.PolicyRule{
			APIGroups: []string{""},
			Verbs:     []string{"get", "list", "watch"},
			Resources: []string{"configmaps", "secrets"},
		})
	}
	if !ptr.Deref(cr.Spec.IngestOnlyMode, false) {
		rules = append(rules, []rbacv1.PolicyRule{
			{
				APIGroups: []string{"discovery.k8s.io"},
				Verbs:     []string{"get", "list", "watch"},
				Resources: []string{"endpointslices"},
			},
			{
				APIGroups: []string{""},
				Verbs:     []string{"get", "list", "watch"},
				Resources: []string{"services", "endpoints", "pods"},
			},
			{
				APIGroups: []string{"networking.k8s.io", "extensions"},
				Verbs:     []string{"get", "list", "watch"},
				Resources: []string{"ingresses"},
			},
		}...)
	}
	return rules
}

func getClusterWideRules(cr *vmv1beta1.VMAgent) []rbacv1.PolicyRule {
	var rules []rbacv1.PolicyRule
	if !ptr.Deref(cr.Spec.IngestOnlyMode, false) || cr.HasAnyRelabellingConfigs() || cr.HasAnyStreamAggrRule() {
		rules = append(rules, rbacv1.PolicyRule{
			APIGroups: []string{""},
			Verbs:     []string{"get", "list", "watch"},
			Resources: []string{"configmaps", "secrets"},
		})
	}
	if !ptr.Deref(cr.Spec.IngestOnlyMode, false) {
		rules = append(rules, []rbacv1.PolicyRule{
			{
				APIGroups: []string{"discovery.k8s.io"},
				Verbs:     []string{"get", "list", "watch"},
				Resources: []string{"endpointslices"},
			},
			{
				APIGroups: []string{""},
				Verbs:     []string{"get", "list", "watch"},
				Resources: []string{"nodes", "nodes/metrics", "services", "endpoints", "pods", "namespaces"},
			},
			{
				APIGroups: []string{"networking.k8s.io", "extensions"},
				Verbs:     []string{"get", "list", "watch"},
				Resources: []string{"ingresses"},
			},
			{
				NonResourceURLs: []string{"/metrics", "/metrics/resources", "/metrics/slis"},
				Verbs:           []string{"get", "list", "watch"},
			},
			{
				APIGroups: []string{"route.openshift.io", "image.openshift.io"},
				Verbs:     []string{"get"},
				Resources: []string{"routers/metrics", "registry/metrics"},
			},
		}...)
	}
	return rules
}

// createK8sAPIAccess - creates RBAC access rules for vmagent
func createK8sAPIAccess(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMAgent, clusterWide bool) error {
	if err := migrateRBAC(ctx, rclient, cr, clusterWide); err != nil {
		return fmt.Errorf("cannot perform RBAC migration: %w", err)
	}
	if clusterWide {
		if err := ensureCRExist(ctx, rclient, cr, prevCR); err != nil {
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
	if err := ensureRBExist(ctx, rclient, cr, prevCR); err != nil {
		return fmt.Errorf("cannot ensure state of vmagent's role binding: %w", err)
	}

	return nil
}

func ensureCRExist(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMAgent) error {
	var prevClusterRole *rbacv1.ClusterRole
	if prevCR != nil {
		prevClusterRole = buildCR(prevCR)
	}
	return reconcile.ClusterRole(ctx, rclient, buildCR(cr), prevClusterRole)
}

func ensureCRBExist(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMAgent) error {
	var prevCRB *rbacv1.ClusterRoleBinding
	if prevCR != nil {
		prevCRB = buildCRB(prevCR)
	}
	return reconcile.ClusterRoleBinding(ctx, rclient, buildCRB(cr), prevCRB)
}

// migrateRBAC deletes incorrectly formatted resource names
// see https://github.com/VictoriaMetrics/operator/issues/891
// and https://github.com/VictoriaMetrics/operator/pull/1176
func migrateRBAC(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAgent, clusterWide bool) error {
	// explicitly set namespace via ObjetMeta for unit tests
	objMeta := metav1.ObjectMeta{
		Name: "monitoring:vmagent-cluster-access-" + cr.Name,
	}
	objsToMigrate := []client.Object{
		&rbacv1.ClusterRole{ObjectMeta: objMeta},
		&rbacv1.ClusterRoleBinding{ObjectMeta: objMeta},
	}
	if !clusterWide {
		objMeta.Namespace = cr.Namespace
		objsToMigrate = []client.Object{
			&rbacv1.Role{ObjectMeta: objMeta},
			&rbacv1.RoleBinding{ObjectMeta: objMeta},
		}
	}
	owner := cr.AsOwner()
	return finalize.SafeDeleteWithFinalizer(ctx, rclient, objsToMigrate, cr.SelectorLabels(), &owner)
}

func buildCRB(cr *vmv1beta1.VMAgent) *rbacv1.ClusterRoleBinding {
	return &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:        cr.GetClusterRoleName(),
			Labels:      cr.FinalLabels(),
			Annotations: cr.FinalAnnotations(),
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

func buildCR(cr *vmv1beta1.VMAgent) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:        cr.GetClusterRoleName(),
			Labels:      cr.FinalLabels(),
			Annotations: cr.FinalAnnotations(),
		},
		Rules: getClusterWideRules(cr),
	}
}

func ensureRoleExist(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMAgent) error {
	nr := buildRole(cr)
	var prevRole *rbacv1.Role
	if prevCR != nil {
		prevRole = buildRole(prevCR)
	}
	owner := cr.AsOwner()
	return reconcile.Role(ctx, rclient, nr, prevRole, &owner)
}

func ensureRBExist(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMAgent) error {
	rb := buildRB(cr)
	var prevRB *rbacv1.RoleBinding
	if prevCR != nil {
		prevRB = buildRB(prevCR)
	}
	owner := cr.AsOwner()
	return reconcile.RoleBinding(ctx, rclient, rb, prevRB, &owner)
}

func buildRole(cr *vmv1beta1.VMAgent) *rbacv1.Role {
	return &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.GetClusterRoleName(),
			Namespace:       cr.GetNamespace(),
			Labels:          cr.FinalLabels(),
			Annotations:     cr.FinalAnnotations(),
			OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
		},
		Rules: getSingleNamespaceRules(cr),
	}
}

func buildRB(cr *vmv1beta1.VMAgent) *rbacv1.RoleBinding {
	return &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.GetClusterRoleName(),
			Namespace:       cr.GetNamespace(),
			Labels:          cr.FinalLabels(),
			Annotations:     cr.FinalAnnotations(),
			OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
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
