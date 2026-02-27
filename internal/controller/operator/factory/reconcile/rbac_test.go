package reconcile

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestRoleBindingReconcile(t *testing.T) {
	type opts struct {
		new, prev         *rbacv1.RoleBinding
		predefinedObjects []runtime.Object
		actions           []k8stools.ClientAction
	}
	getRoleBinding := func(fns ...func(rb *rbacv1.RoleBinding)) *rbacv1.RoleBinding {
		rb := &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-rb",
				Namespace: "default",
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "Role",
				Name:     "test-role",
			},
			Subjects: []rbacv1.Subject{
				{
					Kind:      "ServiceAccount",
					Name:      "test-sa",
					Namespace: "default",
				},
			},
		}
		for _, fn := range fns {
			fn(rb)
		}
		return rb
	}

	f := func(o opts) {
		t.Helper()
		ctx := context.Background()
		cl := k8stools.GetTestClientWithActions(o.predefinedObjects)
		assert.NoError(t, RoleBinding(ctx, cl, o.new, o.prev, nil))
		assert.Equal(t, o.actions, cl.Actions)
	}

	nn := types.NamespacedName{Name: "test-rb", Namespace: "default"}

	// create
	f(opts{
		new: getRoleBinding(),
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "RoleBinding", Resource: nn},
			{Verb: "Create", Kind: "RoleBinding", Resource: nn},
		},
	})

	// no updates
	f(opts{
		new:  getRoleBinding(),
		prev: getRoleBinding(),
		predefinedObjects: []runtime.Object{
			getRoleBinding(),
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "RoleBinding", Resource: nn},
		},
	})

	// update subjects
	f(opts{
		new: getRoleBinding(func(rb *rbacv1.RoleBinding) {
			rb.Subjects = append(rb.Subjects, rbacv1.Subject{Kind: "User", Name: "test-user"})
		}),
		prev: getRoleBinding(),
		predefinedObjects: []runtime.Object{
			getRoleBinding(),
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "RoleBinding", Resource: nn},
			{Verb: "Update", Kind: "RoleBinding", Resource: nn},
		},
	})
}

func TestRoleReconcile(t *testing.T) {
	type opts struct {
		new, prev         *rbacv1.Role
		predefinedObjects []runtime.Object
		actions           []k8stools.ClientAction
	}
	getRole := func(fns ...func(r *rbacv1.Role)) *rbacv1.Role {
		r := &rbacv1.Role{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-role",
				Namespace: "default",
			},
			Rules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"get"},
					Resources: []string{"pods"},
					APIGroups: []string{""},
				},
			},
		}
		for _, fn := range fns {
			fn(r)
		}
		return r
	}

	f := func(o opts) {
		t.Helper()
		ctx := context.Background()
		cl := k8stools.GetTestClientWithActions(o.predefinedObjects)
		assert.NoError(t, Role(ctx, cl, o.new, o.prev, nil))
		assert.Equal(t, o.actions, cl.Actions)
	}

	nn := types.NamespacedName{Name: "test-role", Namespace: "default"}

	// create
	f(opts{
		new: getRole(),
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "Role", Resource: nn},
			{Verb: "Create", Kind: "Role", Resource: nn},
		},
	})

	// no updates
	f(opts{
		new:  getRole(),
		prev: getRole(),
		predefinedObjects: []runtime.Object{
			getRole(),
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "Role", Resource: nn},
		},
	})

	// update rules
	f(opts{
		new: getRole(func(r *rbacv1.Role) {
			r.Rules = append(r.Rules, rbacv1.PolicyRule{Verbs: []string{"list"}, Resources: []string{"services"}})
		}),
		prev: getRole(),
		predefinedObjects: []runtime.Object{
			getRole(),
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "Role", Resource: nn},
			{Verb: "Update", Kind: "Role", Resource: nn},
		},
	})
}

func TestClusterRoleBindingReconcile(t *testing.T) {
	type opts struct {
		new, prev         *rbacv1.ClusterRoleBinding
		predefinedObjects []runtime.Object
		actions           []k8stools.ClientAction
	}
	getCRB := func(fns ...func(crb *rbacv1.ClusterRoleBinding)) *rbacv1.ClusterRoleBinding {
		crb := &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-crb",
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     "test-cr",
			},
			Subjects: []rbacv1.Subject{
				{
					Kind:      "ServiceAccount",
					Name:      "test-sa",
					Namespace: "default",
				},
			},
		}
		for _, fn := range fns {
			fn(crb)
		}
		return crb
	}

	f := func(o opts) {
		t.Helper()
		ctx := context.Background()
		cl := k8stools.GetTestClientWithActions(o.predefinedObjects)
		assert.NoError(t, ClusterRoleBinding(ctx, cl, o.new, o.prev))
		assert.Equal(t, o.actions, cl.Actions)
	}

	nn := types.NamespacedName{Name: "test-crb"}

	// create
	f(opts{
		new: getCRB(),
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "ClusterRoleBinding", Resource: nn},
			{Verb: "Create", Kind: "ClusterRoleBinding", Resource: nn},
		},
	})

	// no updates
	f(opts{
		new:  getCRB(),
		prev: getCRB(),
		predefinedObjects: []runtime.Object{
			getCRB(),
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "ClusterRoleBinding", Resource: nn},
		},
	})

	// update subjects
	f(opts{
		new: getCRB(func(crb *rbacv1.ClusterRoleBinding) {
			crb.Subjects = append(crb.Subjects, rbacv1.Subject{Kind: "User", Name: "test-user"})
		}),
		prev: getCRB(),
		predefinedObjects: []runtime.Object{
			getCRB(),
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "ClusterRoleBinding", Resource: nn},
			{Verb: "Update", Kind: "ClusterRoleBinding", Resource: nn},
		},
	})
}

func TestClusterRoleReconcile(t *testing.T) {
	type opts struct {
		new, prev         *rbacv1.ClusterRole
		predefinedObjects []runtime.Object
		actions           []k8stools.ClientAction
	}
	getClusterRole := func(fns ...func(cr *rbacv1.ClusterRole)) *rbacv1.ClusterRole {
		cr := &rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-cr",
			},
			Rules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"get"},
					Resources: []string{"pods"},
					APIGroups: []string{""},
				},
			},
		}
		for _, fn := range fns {
			fn(cr)
		}
		return cr
	}

	f := func(o opts) {
		t.Helper()
		ctx := context.Background()
		cl := k8stools.GetTestClientWithActions(o.predefinedObjects)
		assert.NoError(t, ClusterRole(ctx, cl, o.new, o.prev))
		assert.Equal(t, o.actions, cl.Actions)
	}

	nn := types.NamespacedName{Name: "test-cr"}

	// create
	f(opts{
		new: getClusterRole(),
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "ClusterRole", Resource: nn},
			{Verb: "Create", Kind: "ClusterRole", Resource: nn},
		},
	})

	// no updates
	f(opts{
		new:  getClusterRole(),
		prev: getClusterRole(),
		predefinedObjects: []runtime.Object{
			getClusterRole(),
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "ClusterRole", Resource: nn},
		},
	})

	// update rules
	f(opts{
		new: getClusterRole(func(cr *rbacv1.ClusterRole) {
			cr.Rules = append(cr.Rules, rbacv1.PolicyRule{Verbs: []string{"list"}, Resources: []string{"services"}})
		}),
		prev: getClusterRole(),
		predefinedObjects: []runtime.Object{
			getClusterRole(),
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "ClusterRole", Resource: nn},
			{Verb: "Update", Kind: "ClusterRole", Resource: nn},
		},
	})
}
