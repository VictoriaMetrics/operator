package vmagent

import (
	"context"
	"fmt"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

// getScrapeDiscoveryRules returns the K8s service-discovery rules needed in every watched namespace.
func getScrapeDiscoveryRules() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
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
	}
}

// getSingleNamespaceRules returns rules for the Role that lives in cr's own namespace.
// It includes a scoped secrets rule for the config-reloader plus the SD discovery rules.
func getSingleNamespaceRules(cr *vmv1beta1.VMAgent) []rbacv1.PolicyRule {
	ingestOnly := ptr.Deref(cr.Spec.IngestOnlyMode, false)
	if ingestOnly && !hasRemoteWriteSecrets(cr) {
		return nil
	}
	secretsRule := rbacv1.PolicyRule{
		APIGroups:     []string{""},
		Verbs:         []string{"get", "watch"},
		Resources:     []string{"secrets"},
		ResourceNames: []string{cr.PrefixedName()},
	}
	if ingestOnly {
		return []rbacv1.PolicyRule{secretsRule}
	}
	return append([]rbacv1.PolicyRule{secretsRule}, getScrapeDiscoveryRules()...)
}

func getClusterWideRules(cr *vmv1beta1.VMAgent) []rbacv1.PolicyRule {
	if ptr.Deref(cr.Spec.IngestOnlyMode, false) {
		return nil
	}
	return []rbacv1.PolicyRule{
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
			Verbs:           []string{"get"},
		},
		{
			APIGroups: []string{"route.openshift.io", "image.openshift.io"},
			Verbs:     []string{"get"},
			Resources: []string{"routers/metrics", "registry/metrics"},
		},
	}
}

// createK8sAPIAccess creates RBAC access rules for vmagent.
// namespaces is the list of watched namespaces; an empty slice means cluster-wide access.
func createK8sAPIAccess(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMAgent, namespaces []string) error {
	if len(namespaces) == 0 {
		if err := ensureCRExist(ctx, rclient, cr, prevCR); err != nil {
			return fmt.Errorf("cannot ensure state of vmagent's cluster role: %w", err)
		}
		if err := ensureCRBExist(ctx, rclient, cr, prevCR); err != nil {
			return fmt.Errorf("cannot ensure state of vmagent's cluster role binding: %w", err)
		}

		// Secrets access must be namespace-scoped even in cluster-wide mode to prevent
		// reading credentials from namespaces other than the vmagent's own namespace.
		if err := ensureRoleExist(ctx, rclient, cr, prevCR, cr.Namespace); err != nil {
			return fmt.Errorf("cannot ensure state of vmagent's role: %w", err)
		}
		if err := ensureRBExist(ctx, rclient, cr, prevCR, cr.Namespace); err != nil {
			return fmt.Errorf("cannot ensure state of vmagent's role binding: %w", err)
		}
		return nil
	}

	for _, ns := range namespaces {
		if err := ensureRoleExist(ctx, rclient, cr, prevCR, ns); err != nil {
			return fmt.Errorf("cannot ensure state of vmagent's role for namespace %s: %w", ns, err)
		}
		if err := ensureRBExist(ctx, rclient, cr, prevCR, ns); err != nil {
			return fmt.Errorf("cannot ensure state of vmagent's role binding for namespace %s: %w", ns, err)
		}
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

func buildCRB(cr *vmv1beta1.VMAgent) *rbacv1.ClusterRoleBinding {
	return &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:        cr.GetRBACName(),
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
			Name:     cr.GetRBACName(),
			Kind:     "ClusterRole",
		},
	}
}

func buildCR(cr *vmv1beta1.VMAgent) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:        cr.GetRBACName(),
			Labels:      cr.FinalLabels(),
			Annotations: cr.FinalAnnotations(),
		},
		Rules: getClusterWideRules(cr),
	}
}

func ensureRoleExist(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMAgent, ns string) error {
	nr := buildRole(cr, ns)
	var prevRole *rbacv1.Role
	if prevCR != nil {
		prevRole = buildRole(prevCR, ns)
	}
	var owner *metav1.OwnerReference
	if ns == cr.Namespace {
		o := cr.AsOwner()
		owner = &o
	}
	return reconcile.Role(ctx, rclient, nr, prevRole, owner)
}

func ensureRBExist(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMAgent, ns string) error {
	rb := buildRB(cr, ns)
	var prevRB *rbacv1.RoleBinding
	if prevCR != nil {
		prevRB = buildRB(prevCR, ns)
	}
	var owner *metav1.OwnerReference
	if ns == cr.Namespace {
		o := cr.AsOwner()
		owner = &o
	}
	return reconcile.RoleBinding(ctx, rclient, rb, prevRB, owner)
}

// buildRole builds the Role for ns.
// In cr's own namespace: includes the scoped secrets rule (for config-reloader) plus SD rules,
// with an owner reference so K8s GC cleans it up.
// In any other watched namespace: SD rules only, no owner reference.
func buildRole(cr *vmv1beta1.VMAgent, ns string) *rbacv1.Role {
	r := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:        cr.GetRBACName(),
			Namespace:   ns,
			Labels:      cr.FinalLabels(),
			Annotations: cr.FinalAnnotations(),
		},
	}
	if ns == cr.Namespace {
		r.OwnerReferences = []metav1.OwnerReference{cr.AsOwner()}
		r.Rules = getSingleNamespaceRules(cr)
	} else if !ptr.Deref(cr.Spec.IngestOnlyMode, false) {
		r.Rules = getScrapeDiscoveryRules()
	}
	return r
}

// buildRB builds the RoleBinding for ns.
// The subject always points to the vmagent service account in cr.Namespace.
// An owner reference is set only in cr's own namespace.
func buildRB(cr *vmv1beta1.VMAgent, ns string) *rbacv1.RoleBinding {
	rb := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:        cr.GetRBACName(),
			Namespace:   ns,
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
			Name:     cr.GetRBACName(),
			Kind:     "Role",
		},
	}
	if ns == cr.Namespace {
		rb.OwnerReferences = []metav1.OwnerReference{cr.AsOwner()}
	}
	return rb
}
