package vmanomaly

import (
	"context"
	"fmt"

	rbacv1 "k8s.io/api/rbac/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

// createK8sAPIAccess - creates RBAC access rules for vmanomaly
func createK8sAPIAccess(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VMAnomaly, clusterWide bool) error {
	if err := migrateRBAC(ctx, rclient, cr, clusterWide); err != nil {
		return fmt.Errorf("cannot perform RBAC migration: %w", err)
	}
	if clusterWide {
		if err := ensureCRExist(ctx, rclient, cr, prevCR); err != nil {
			return fmt.Errorf("cannot ensure state of vmanomaly's cluster role: %w", err)
		}
		if err := ensureCRBExist(ctx, rclient, cr, prevCR); err != nil {
			return fmt.Errorf("cannot ensure state of vmanomaly's cluster role binding: %w", err)
		}
		return nil
	}

	if err := ensureRoleExist(ctx, rclient, cr, prevCR); err != nil {
		return fmt.Errorf("cannot ensure state of vmanomaly's cluster role: %w", err)
	}
	if err := ensureRBExist(ctx, rclient, cr, prevCR); err != nil {
		return fmt.Errorf("cannot ensure state of vmanomaly's role binding: %w", err)
	}

	return nil
}

func ensureCRExist(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VMAnomaly) error {
	var prevClusterRole *rbacv1.ClusterRole
	if prevCR != nil {
		prevClusterRole = buildCR(prevCR)
	}
	return reconcile.ClusterRole(ctx, rclient, buildCR(cr), prevClusterRole)
}

func ensureCRBExist(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VMAnomaly) error {
	var prevCRB *rbacv1.ClusterRoleBinding
	if prevCR != nil {
		prevCRB = buildCRB(prevCR)
	}
	return reconcile.ClusterRoleBinding(ctx, rclient, buildCRB(cr), prevCRB)
}

// migrateRBAC deletes incorrectly formatted resource names
// see https://github.com/VictoriaMetrics/operator/issues/891
// and https://github.com/VictoriaMetrics/operator/pull/1176
func migrateRBAC(ctx context.Context, rclient client.Client, cr *vmv1.VMAnomaly, clusterWide bool) error {
	const prevNamingPrefix = "monitoring:vmanomaly-cluster-access-"
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
			if !k8serrors.IsNotFound(err) {
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

func buildCRB(cr *vmv1.VMAnomaly) *rbacv1.ClusterRoleBinding {
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

func buildCR(cr *vmv1.VMAnomaly) *rbacv1.ClusterRole {
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
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Verbs: []string{
					"get",
					"list",
					"watch",
				},
				Resources: []string{
					"configmaps",
					"namespaces",
					"secrets",
				},
			},
		},
	}
}

func ensureRoleExist(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VMAnomaly) error {
	nr := buildRole(cr)
	var prevRole *rbacv1.Role
	if prevCR != nil {
		prevRole = buildRole(prevCR)
	}
	return reconcile.Role(ctx, rclient, nr, prevRole)
}

func ensureRBExist(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VMAnomaly) error {
	rb := buildRB(cr)
	var prevRB *rbacv1.RoleBinding
	if prevCR != nil {
		prevRB = buildRB(prevCR)
	}
	return reconcile.RoleBinding(ctx, rclient, rb, prevRB)
}

func buildRole(cr *vmv1.VMAnomaly) *rbacv1.Role {
	return &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.GetClusterRoleName(),
			Namespace:       cr.GetNamespace(),
			Labels:          cr.AllLabels(),
			Annotations:     cr.AnnotationsFiltered(),
			Finalizers:      []string{vmv1beta1.FinalizerName},
			OwnerReferences: cr.AsOwner(),
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Verbs: []string{
					"get",
					"list",
					"watch",
				},
				Resources: []string{
					"secrets",
					"configmaps",
				},
			},
		},
	}
}

func buildRB(cr *vmv1.VMAnomaly) *rbacv1.RoleBinding {
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
