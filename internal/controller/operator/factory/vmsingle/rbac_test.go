package vmsingle

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

	// create cluster-wide rbac for scraping mode (IngestOnlyMode=false)
	f(opts{
		cr: &vmv1beta1.VMSingle{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "rbac-test",
			},
			Spec: vmv1beta1.VMSingleSpec{
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					IngestOnlyMode: ptr.To(false),
				},
			},
		},
		namespaces: []string{},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMSingle) {
			var got rbacv1.ClusterRole
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Name: cr.GetRBACName()}, &got))
			assert.Len(t, got.Rules, 5)
		},
	})

	// create namespaced rbac for scraping mode (IngestOnlyMode=false)
	f(opts{
		cr: &vmv1beta1.VMSingle{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "rbac-test",
			},
			Spec: vmv1beta1.VMSingleSpec{
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					IngestOnlyMode: ptr.To(false),
				},
			},
		},
		namespaces: []string{"default"},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMSingle) {
			var got rbacv1.Role
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Name: cr.GetRBACName(), Namespace: cr.Namespace}, &got))
			assert.Len(t, got.Rules, 4)
			var secretsRule *rbacv1.PolicyRule
			for i, rule := range got.Rules {
				if len(rule.Resources) == 1 && rule.Resources[0] == "secrets" {
					secretsRule = &got.Rules[i]
				}
			}
			if assert.NotNil(t, secretsRule, "expected a secrets rule in the namespaced Role") {
				assert.Equal(t, []string{"get", "watch", "list"}, secretsRule.Verbs)
				assert.Equal(t, []string{cr.PrefixedName()}, secretsRule.ResourceNames)
			}
		},
	})

	// ingest-only mode: no cluster-wide rules (relabeling/streamaggr configs are in ConfigMaps, not Secrets)
	f(opts{
		cr: &vmv1beta1.VMSingle{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "rbac-test",
			},
			Spec: vmv1beta1.VMSingleSpec{
				CommonRelabelParams: vmv1beta1.CommonRelabelParams{
					InlineRelabelConfig: []*vmv1beta1.RelabelConfig{{
						Action:       "drop",
						SourceLabels: []string{"environment"},
					}},
				},
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					IngestOnlyMode: ptr.To(true),
				},
			},
		},
		namespaces: []string{},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMSingle) {
			var got rbacv1.ClusterRole
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Name: cr.GetRBACName()}, &got))
			assert.Empty(t, got.Rules)
		},
	})

	// ingest-only namespaced: namespace role has no rules
	f(opts{
		cr: &vmv1beta1.VMSingle{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "rbac-test",
			},
			Spec: vmv1beta1.VMSingleSpec{
				CommonRelabelParams: vmv1beta1.CommonRelabelParams{
					InlineRelabelConfig: []*vmv1beta1.RelabelConfig{{
						Action:       "drop",
						SourceLabels: []string{"environment"},
					}},
				},
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					IngestOnlyMode: ptr.To(true),
				},
			},
		},
		namespaces: []string{"default"},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMSingle) {
			var got rbacv1.Role
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Name: cr.GetRBACName(), Namespace: cr.Namespace}, &got))
			assert.Empty(t, got.Rules)
		},
	})

	// default ingest-only (nil IngestOnlyMode defaults to true): cluster-wide has no rules
	f(opts{
		cr: &vmv1beta1.VMSingle{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "rbac-test",
			},
			Spec: vmv1beta1.VMSingleSpec{},
		},
		namespaces: []string{},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMSingle) {
			var got rbacv1.ClusterRole
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Name: cr.GetRBACName()}, &got))
			assert.Empty(t, got.Rules)
		},
	})

	// default ingest-only (nil IngestOnlyMode defaults to true): namespace role has no rules
	f(opts{
		cr: &vmv1beta1.VMSingle{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "rbac-test",
			},
			Spec: vmv1beta1.VMSingleSpec{},
		},
		namespaces: []string{"default"},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMSingle) {
			var got rbacv1.Role
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Name: cr.GetRBACName(), Namespace: cr.Namespace}, &got))
			assert.Empty(t, got.Rules)
		},
	})

	// ok with existing rbac
	f(opts{
		cr: &vmv1beta1.VMSingle{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default-2",
				Name:      "rbac-test",
			},
			Spec: vmv1beta1.VMSingleSpec{
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					IngestOnlyMode: ptr.To(false),
				},
			},
		},
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
