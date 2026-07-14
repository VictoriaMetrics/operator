package vmagent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestCreateVMAgentRBAC(t *testing.T) {
	type opts struct {
		cr                *vmv1beta1.VMAgent
		validate          func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAgent)
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

	// create default cluster-wide rbac: ClusterRole has SD rules only (no secrets);
	// a namespace-scoped Role in cr.Namespace carries the secrets rule.
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "rbac-test",
			},
			Spec: vmv1beta1.VMAgentSpec{},
		},
		namespaces: nil,
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAgent) {
			var cr1 rbacv1.ClusterRole
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Name: cr.GetRBACName()}, &cr1))
			assert.Len(t, cr1.Rules, 5)
			for _, rule := range cr1.Rules {
				for _, res := range rule.Resources {
					assert.NotEqual(t, "secrets", res, "ClusterRole must not include secrets")
				}
			}
			var role rbacv1.Role
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Name: cr.GetRBACName(), Namespace: cr.Namespace}, &role))
			assert.Len(t, role.Rules, 4)
		},
	})

	// create namespaced rbac (single watched namespace == cr.Namespace)
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "rbac-test",
			},
			Spec: vmv1beta1.VMAgentSpec{},
		},
		namespaces: []string{"default"},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAgent) {
			var got rbacv1.Role
			nsn := types.NamespacedName{
				Name:      cr.GetRBACName(),
				Namespace: cr.Namespace,
			}
			assert.NoError(t, rclient.Get(ctx, nsn, &got))
			assert.Len(t, got.Rules, 4)
		},
	})

	// multi-namespace: primary namespace gets secrets+SD rules; other namespace gets SD rules only
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "rbac-test",
			},
			Spec: vmv1beta1.VMAgentSpec{},
		},
		namespaces: []string{"default", "other-ns"},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAgent) {
			var primary rbacv1.Role
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Name: cr.GetRBACName(), Namespace: "default"}, &primary))
			assert.Len(t, primary.Rules, 4)

			var other rbacv1.Role
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Name: cr.GetRBACName(), Namespace: "other-ns"}, &other))
			assert.Len(t, other.Rules, 3)
			for _, rule := range other.Rules {
				assert.Empty(t, rule.ResourceNames, "other-ns role must not restrict by resourceNames")
				for _, res := range rule.Resources {
					assert.NotEqual(t, "secrets", res, "other-ns role must not include secrets")
				}
			}
		},
	})

	// cluster-wide rbac for ingest-only with relabeling: no K8s SD → no rules
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "rbac-test",
			},
			Spec: vmv1beta1.VMAgentSpec{
				RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{{
					InlineUrlRelabelConfig: []*vmv1beta1.RelabelConfig{
						{
							Action:       "drop",
							SourceLabels: []string{"environment"},
						},
					},
				}},
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					IngestOnlyMode: ptr.To(true),
				},
			},
		},
		namespaces: nil,
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAgent) {
			var got rbacv1.ClusterRole
			nsn := types.NamespacedName{
				Name: cr.GetRBACName(),
			}
			assert.NoError(t, rclient.Get(ctx, nsn, &got))
			assert.Empty(t, got.Rules)
		},
	})

	// namespaced rbac for ingest-only with relabeling: no K8s SD → no rules
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "rbac-test",
			},
			Spec: vmv1beta1.VMAgentSpec{
				RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{{
					InlineUrlRelabelConfig: []*vmv1beta1.RelabelConfig{
						{
							Action:       "drop",
							SourceLabels: []string{"environment"},
						},
					},
				}},
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					IngestOnlyMode: ptr.To(true),
				},
			},
		},
		namespaces: []string{"default"},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAgent) {
			var got rbacv1.Role
			nsn := types.NamespacedName{
				Name:      cr.GetRBACName(),
				Namespace: cr.Namespace,
			}
			assert.NoError(t, rclient.Get(ctx, nsn, &got))
			assert.Empty(t, got.Rules)
		},
	})

	// cluster-wide rbac with no rules
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "rbac-test",
			},
			Spec: vmv1beta1.VMAgentSpec{
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					IngestOnlyMode: ptr.To(true),
				},
			},
		},
		namespaces: nil,
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAgent) {
			var got rbacv1.ClusterRole
			nsn := types.NamespacedName{
				Name: cr.GetRBACName(),
			}
			assert.NoError(t, rclient.Get(ctx, nsn, &got))
			assert.Empty(t, got.Rules)
		},
	})

	// namespaced rbac for ingest-only: no rules
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "rbac-test",
			},
			Spec: vmv1beta1.VMAgentSpec{
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					IngestOnlyMode: ptr.To(true),
				},
			},
		},
		namespaces: []string{"default"},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAgent) {
			var got rbacv1.Role
			nsn := types.NamespacedName{
				Name:      cr.GetRBACName(),
				Namespace: cr.Namespace,
			}
			assert.NoError(t, rclient.Get(ctx, nsn, &got))
			assert.Empty(t, got.Rules)
		},
	})

	// cluster-wide ingest-only with bearer token secret: Role in cr.Namespace has secrets rule only
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "rbac-test",
			},
			Spec: vmv1beta1.VMAgentSpec{
				RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{{
					URL: "http://remote:8428",
					BearerTokenSecret: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "my-token"},
						Key:                  "token",
					},
				}},
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					IngestOnlyMode: ptr.To(true),
				},
			},
		},
		namespaces: nil,
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAgent) {
			var clusterRole rbacv1.ClusterRole
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Name: cr.GetRBACName()}, &clusterRole))
			assert.Empty(t, clusterRole.Rules, "ClusterRole must have no rules in ingest-only mode")

			var role rbacv1.Role
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Name: cr.GetRBACName(), Namespace: cr.Namespace}, &role))
			assert.Len(t, role.Rules, 1, "Role must have secrets rule only")
			assert.Equal(t, []string{"secrets"}, role.Rules[0].Resources)
			assert.Equal(t, []string{cr.PrefixedName()}, role.Rules[0].ResourceNames)
			assert.Equal(t, []string{"get", "watch", "list"}, role.Rules[0].Verbs)
		},
	})

	// namespaced ingest-only with basic auth secret: Role has secrets rule only (no SD rules)
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "rbac-test",
			},
			Spec: vmv1beta1.VMAgentSpec{
				RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{{
					URL: "http://remote:8428",
					BasicAuth: &vmv1beta1.BasicAuth{
						Password: corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: "my-secret"},
							Key:                  "password",
						},
					},
				}},
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					IngestOnlyMode: ptr.To(true),
				},
			},
		},
		namespaces: []string{"default"},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAgent) {
			var role rbacv1.Role
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Name: cr.GetRBACName(), Namespace: cr.Namespace}, &role))
			assert.Len(t, role.Rules, 1, "Role must have secrets rule only")
			assert.Equal(t, []string{"secrets"}, role.Rules[0].Resources)
			assert.Equal(t, []string{cr.PrefixedName()}, role.Rules[0].ResourceNames)
			for _, rule := range role.Rules {
				for _, res := range rule.Resources {
					assert.NotEqual(t, "services", res, "ingest-only role must not include SD resources")
				}
			}
		},
	})

	// ok with exist rbac
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default-2",
				Name:      "rbac-test",
			},
			Spec: vmv1beta1.VMAgentSpec{},
		},
		predefinedObjects: []runtime.Object{
			&rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{
					Name: "monitoring:vmagent-cluster-access-rbac-test",
					Labels: map[string]string{
						"app.kubernetes.io/name":      "vmagent",
						"app.kubernetes.io/instance":  "rbac-test",
						"app.kubernetes.io/component": "monitoring",
						"managed-by":                  "vm-operator",
					},
				},
			},
			&rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: "monitoring:vmagent-cluster-access-rbac-test",
					Labels: map[string]string{
						"app.kubernetes.io/name":      "vmagent",
						"app.kubernetes.io/instance":  "rbac-test",
						"app.kubernetes.io/component": "monitoring",
						"managed-by":                  "vm-operator",
					},
				},
			},
			&corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vmagent-rbac-test",
					Namespace: "default-2",
				},
			},
		},
	})
}
