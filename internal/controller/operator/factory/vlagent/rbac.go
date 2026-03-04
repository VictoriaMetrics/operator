package vlagent

import (
	"context"
	"fmt"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

var (
	policyRules = []rbacv1.PolicyRule{{
		APIGroups: []string{""},
		Verbs: []string{
			"get",
			"list",
			"watch",
		},
		Resources: []string{
			"pods",
			"namespaces",
			"nodes",
		},
	}}
)

// createK8sAPIAccess - creates RBAC access rules for vlagent
func createK8sAPIAccess(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VLAgent) error {
	if !config.IsClusterWideAccessAllowed() {
		logger.WithContext(ctx).Info(fmt.Sprintf("skipping cluster role and binding for vlagent=%s/%s since operator has WATCH_NAMESPACE set", cr.Namespace, cr.Name))
		return nil
	}
	if err := ensureCRExist(ctx, rclient, cr, prevCR); err != nil {
		return fmt.Errorf("cannot ensure state of vlagent's cluster role: %w", err)
	}
	if err := ensureCRBExist(ctx, rclient, cr, prevCR); err != nil {
		return fmt.Errorf("cannot ensure state of vlagent's cluster role binding: %w", err)
	}
	return nil
}

func ensureCRExist(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VLAgent) error {
	var prevClusterRole *rbacv1.ClusterRole
	if prevCR != nil {
		prevClusterRole = buildCR(prevCR)
	}
	return reconcile.ClusterRole(ctx, rclient, buildCR(cr), prevClusterRole)
}

func ensureCRBExist(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VLAgent) error {
	var prevCRB *rbacv1.ClusterRoleBinding
	if prevCR != nil {
		prevCRB = buildCRB(prevCR)
	}
	return reconcile.ClusterRoleBinding(ctx, rclient, buildCRB(cr), prevCRB)
}

func buildCRB(cr *vmv1.VLAgent) *rbacv1.ClusterRoleBinding {
	return &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:        cr.GetRBACName(),
			Labels:      cr.FinalLabels(),
			Annotations: cr.FinalAnnotations(),
		},
		Subjects: []rbacv1.Subject{{
			Kind:      rbacv1.ServiceAccountKind,
			Name:      cr.GetServiceAccountName(),
			Namespace: cr.GetNamespace(),
		}},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Name:     cr.GetRBACName(),
			Kind:     "ClusterRole",
		},
	}
}

func buildCR(cr *vmv1.VLAgent) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:        cr.GetRBACName(),
			Labels:      cr.FinalLabels(),
			Annotations: cr.FinalAnnotations(),
		},
		Rules: policyRules,
	}
}
