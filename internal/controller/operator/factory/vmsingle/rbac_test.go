package vmsingle

import (
	"context"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestCreateVMSingleRBAC(t *testing.T) {
	type opts struct {
		cr                *vmv1beta1.VMSingle
		validate          func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMSingle)
		namespaces        []string
		predefinedObjects []runtime.Object
	}
	f := func(o opts) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		ctx := context.TODO()
		assert.NoError(t, createK8sAPIAccess(ctx, fclient, o.cr, nil, o.namespaces))
		if o.validate != nil {
			o.validate(ctx, fclient, o.cr)
		}
	}
	newCR := func(namespace string) *vmv1beta1.VMSingle {
		return &vmv1beta1.VMSingle{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      "rbac-test",
			},
			Spec: vmv1beta1.VMSingleSpec{},
		}
	}
	hasResource := func(rules []rbacv1.PolicyRule, resource string) bool {
		for _, rule := range rules {
			if slices.Contains(rule.Resources, resource) {
				return true
			}
		}
		return false
	}

	// cluster-wide rbac keeps secret access namespace-scoped.
	f(opts{
		cr:         newCR("default"),
		namespaces: nil,
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMSingle) {
			var clusterRole rbacv1.ClusterRole
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Name: cr.GetRBACName()}, &clusterRole))
			assert.Len(t, clusterRole.Rules, 5)
			assert.False(t, hasResource(clusterRole.Rules, "secrets"), "ClusterRole must not include secrets")

			var clusterRoleBinding rbacv1.ClusterRoleBinding
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Name: cr.GetRBACName()}, &clusterRoleBinding))
			assert.Equal(t, cr.GetServiceAccountName(), clusterRoleBinding.Subjects[0].Name)
			assert.Equal(t, cr.Namespace, clusterRoleBinding.Subjects[0].Namespace)

			var role rbacv1.Role
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Name: cr.GetRBACName(), Namespace: cr.Namespace}, &role))
			assert.Len(t, role.Rules, 4)
			assert.Equal(t, []string{"secrets"}, role.Rules[0].Resources)
			assert.Equal(t, []string{cr.PrefixedName()}, role.Rules[0].ResourceNames)

			var roleBinding rbacv1.RoleBinding
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Name: cr.GetRBACName(), Namespace: cr.Namespace}, &roleBinding))
			assert.Equal(t, cr.GetServiceAccountName(), roleBinding.Subjects[0].Name)
			assert.Equal(t, cr.Namespace, roleBinding.Subjects[0].Namespace)
		},
	})

	// namespaced rbac creates only Role/RoleBinding.
	f(opts{
		cr:         newCR("default"),
		namespaces: []string{"default"},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMSingle) {
			var clusterRole rbacv1.ClusterRole
			err := rclient.Get(ctx, types.NamespacedName{Name: cr.GetRBACName()}, &clusterRole)
			assert.True(t, k8serrors.IsNotFound(err))

			var clusterRoleBinding rbacv1.ClusterRoleBinding
			err = rclient.Get(ctx, types.NamespacedName{Name: cr.GetRBACName()}, &clusterRoleBinding)
			assert.True(t, k8serrors.IsNotFound(err))

			var role rbacv1.Role
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Name: cr.GetRBACName(), Namespace: cr.Namespace}, &role))
			assert.Len(t, role.Rules, 4)
			assert.Equal(t, []string{"secrets"}, role.Rules[0].Resources)

			var roleBinding rbacv1.RoleBinding
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Name: cr.GetRBACName(), Namespace: cr.Namespace}, &roleBinding))
			assert.Equal(t, "Role", roleBinding.RoleRef.Kind)
		},
	})

	// non-primary watched namespaces get empty unowned Role/RoleBinding.
	f(opts{
		cr:         newCR("default"),
		namespaces: []string{"default", "other-ns"},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMSingle) {
			var primaryRole rbacv1.Role
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Name: cr.GetRBACName(), Namespace: cr.Namespace}, &primaryRole))
			assert.Len(t, primaryRole.Rules, 4)
			assert.NotEmpty(t, primaryRole.OwnerReferences)

			var otherRole rbacv1.Role
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Name: cr.GetRBACName(), Namespace: "other-ns"}, &otherRole))
			assert.Empty(t, otherRole.Rules)
			assert.Empty(t, otherRole.OwnerReferences)

			var otherRoleBinding rbacv1.RoleBinding
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Name: cr.GetRBACName(), Namespace: "other-ns"}, &otherRoleBinding))
			assert.Empty(t, otherRoleBinding.OwnerReferences)
			assert.Equal(t, cr.GetServiceAccountName(), otherRoleBinding.Subjects[0].Name)
			assert.Equal(t, cr.Namespace, otherRoleBinding.Subjects[0].Namespace)
		},
	})

	// ok with existing rbac
	f(opts{
		cr:         newCR("default-2"),
		namespaces: []string{},
		predefinedObjects: []runtime.Object{
			&rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{
					Name: "monitoring:vmsingle-cluster-access-rbac-test",
					Labels: map[string]string{
						"app.kubernetes.io/name":      "vmsingle",
						"app.kubernetes.io/instance":  "rbac-test",
						"app.kubernetes.io/component": "monitoring",
						"managed-by":                  "vm-operator",
					},
				},
			},
			&rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: "monitoring:vmsingle-cluster-access-rbac-test",
					Labels: map[string]string{
						"app.kubernetes.io/name":      "vmsingle",
						"app.kubernetes.io/instance":  "rbac-test",
						"app.kubernetes.io/component": "monitoring",
						"managed-by":                  "vm-operator",
					},
				},
			},
			&corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vmsingle-rbac-test",
					Namespace: "default-2",
				},
			},
		},
	})
}
