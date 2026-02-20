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
		clusterWide       bool
		predefinedObjects []runtime.Object
	}
	f := func(o opts) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		ctx := context.TODO()
		assert.NoError(t, createK8sAPIAccess(ctx, fclient, o.cr, nil, o.clusterWide))
		if o.validate != nil {
			o.validate(ctx, fclient, o.cr)
		}
	}

	// create default cluster-wide rbac
	f(opts{
		cr: &vmv1beta1.VMSingle{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "rbac-test",
			},
			Spec: vmv1beta1.VMSingleSpec{},
		},
		clusterWide: true,
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMSingle) {
			var got rbacv1.ClusterRole
			nsn := types.NamespacedName{
				Name: cr.GetRBACName(),
			}
			assert.NoError(t, rclient.Get(ctx, nsn, &got))
			assert.Len(t, got.Rules, 6)
		},
	})

	// create namespaced rbac
	f(opts{
		cr: &vmv1beta1.VMSingle{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "rbac-test",
			},
			Spec: vmv1beta1.VMSingleSpec{},
		},
		clusterWide: false,
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMSingle) {
			var got rbacv1.Role
			nsn := types.NamespacedName{
				Name:      cr.GetRBACName(),
				Namespace: cr.Namespace,
			}
			assert.NoError(t, rclient.Get(ctx, nsn, &got))
			assert.Len(t, got.Rules, 4)
		},
	})

	// create cluster-wide rbac for relabeling
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
		clusterWide: true,
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMSingle) {
			var got rbacv1.ClusterRole
			nsn := types.NamespacedName{
				Name: cr.GetRBACName(),
			}
			assert.NoError(t, rclient.Get(ctx, nsn, &got))
			assert.Len(t, got.Rules, 1)
		},
	})

	// create namespaced rbac for relabeling
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
		clusterWide: false,
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMSingle) {
			var got rbacv1.Role
			nsn := types.NamespacedName{
				Name:      cr.GetRBACName(),
				Namespace: cr.Namespace,
			}
			assert.NoError(t, rclient.Get(ctx, nsn, &got))
			assert.Len(t, got.Rules, 1)
		},
	})

	// cluster-wide rbac with no rules
	f(opts{
		cr: &vmv1beta1.VMSingle{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "rbac-test",
			},
			Spec: vmv1beta1.VMSingleSpec{
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					IngestOnlyMode: ptr.To(true),
				},
			},
		},
		clusterWide: true,
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMSingle) {
			var got rbacv1.ClusterRole
			nsn := types.NamespacedName{
				Name: cr.GetRBACName(),
			}
			assert.NoError(t, rclient.Get(ctx, nsn, &got))
			assert.Empty(t, got.Rules)
		},
	})

	// no namespaced rbac
	f(opts{
		cr: &vmv1beta1.VMSingle{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "rbac-test",
			},
			Spec: vmv1beta1.VMSingleSpec{
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					IngestOnlyMode: ptr.To(true),
				},
			},
		},
		clusterWide: false,
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMSingle) {
			var got rbacv1.Role
			nsn := types.NamespacedName{
				Name:      cr.GetRBACName(),
				Namespace: cr.Namespace,
			}
			assert.NoError(t, rclient.Get(ctx, nsn, &got))
			assert.Empty(t, got.Rules)
		},
	})

	// ok with exist rbac
	f(opts{
		cr: &vmv1beta1.VMSingle{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default-2",
				Name:      "rbac-test",
			},
			Spec: vmv1beta1.VMSingleSpec{},
		},
		predefinedObjects: []runtime.Object{
			&rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "monitoring:vmsingle-cluster-access-rbac-test",
					Namespace: "default-2",
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
					Name:      "monitoring:vmsingle-cluster-access-rbac-test",
					Namespace: "default-2",
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
