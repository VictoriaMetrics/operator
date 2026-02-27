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
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "rbac-test",
			},
			Spec: vmv1beta1.VMAgentSpec{},
		},
		clusterWide: true,
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAgent) {
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
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "rbac-test",
			},
			Spec: vmv1beta1.VMAgentSpec{},
		},
		clusterWide: false,
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

	// create cluster-wide rbac for relabeling
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
		clusterWide: true,
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAgent) {
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
		clusterWide: false,
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAgent) {
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
		clusterWide: true,
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAgent) {
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
		clusterWide: false,
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
