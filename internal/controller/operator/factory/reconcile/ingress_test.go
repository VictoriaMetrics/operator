package reconcile

import (
	"context"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestIngressReconcile(t *testing.T) {
	type opts struct {
		new, prev         *networkingv1.Ingress
		predefinedObjects []runtime.Object
		actions           []k8stools.ClientAction
	}
	getIngress := func(fns ...func(r *networkingv1.Ingress)) *networkingv1.Ingress {
		ing := &networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-ingress",
				Namespace: "default",
			},
			Spec: networkingv1.IngressSpec{
				Rules: []networkingv1.IngressRule{
					{
						Host: "example.com",
					},
				},
			},
		}
		for _, fn := range fns {
			fn(ing)
		}
		return ing
	}

	f := func(o opts) {
		t.Helper()
		ctx := context.Background()
		cl := k8stools.GetTestClientWithActions(o.predefinedObjects)
		synctest.Test(t, func(t *testing.T) {
			assert.NoError(t, Ingress(ctx, cl, o.new, o.prev, nil))
			assert.Equal(t, o.actions, cl.Actions)
		})
	}

	nn := types.NamespacedName{Name: "test-ingress", Namespace: "default"}

	// create
	f(opts{
		new: getIngress(),
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "Ingress", Resource: nn},
			{Verb: "Create", Kind: "Ingress", Resource: nn},
		},
	})

	// no updates
	f(opts{
		new:  getIngress(),
		prev: getIngress(),
		predefinedObjects: []runtime.Object{
			getIngress(),
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "Ingress", Resource: nn},
		},
	})

	// no update on status change
	f(opts{
		new:  getIngress(),
		prev: getIngress(),
		predefinedObjects: []runtime.Object{
			getIngress(func(ing *networkingv1.Ingress) {
				ing.Status.LoadBalancer.Ingress = []networkingv1.IngressLoadBalancerIngress{
					{
						IP: "127.0.0.1",
					},
				}
			}),
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "Ingress", Resource: nn},
		},
	})

	// update spec
	f(opts{
		new: getIngress(func(ing *networkingv1.Ingress) {
			ing.Spec.Rules = append(ing.Spec.Rules, networkingv1.IngressRule{Host: "example.org"})
		}),
		prev: getIngress(),
		predefinedObjects: []runtime.Object{
			getIngress(),
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "Ingress", Resource: nn},
			{Verb: "Update", Kind: "Ingress", Resource: nn},
		},
	})
}

func TestIngressFieldRemovals(t *testing.T) {
	ingress := func(host string, class *string) *networkingv1.Ingress {
		return &networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			Spec: networkingv1.IngressSpec{
				IngressClassName: class,
				Rules: []networkingv1.IngressRule{{
					Host: host,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{{
								Path: "/", PathType: ptr.To(networkingv1.PathTypePrefix),
								Backend: networkingv1.IngressBackend{
									Service: &networkingv1.IngressServiceBackend{
										Name: "test", Port: networkingv1.ServiceBackendPort{Name: "http"},
									},
								},
							}},
						},
					},
				}},
			},
		}
	}
	f := func(name string, prev, desired, existing, want *networkingv1.Ingress, wantUpdate bool) {
		t.Helper()
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			updates := 0
			cl := k8stools.GetTestClientWithObjectsAndInterceptors([]runtime.Object{existing}, interceptor.Funcs{
				Update: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
					updates++
					return cl.Update(ctx, obj, opts...)
				},
			})
			reconcile := func(prev *networkingv1.Ingress) {
				t.Helper()
				require.NoError(t, Ingress(ctx, cl, desired.DeepCopy(), prev, nil))
				var got networkingv1.Ingress
				require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(desired), &got))
				require.Equal(t, want.Spec, got.Spec, "stored payload")
				require.Equal(t, desired.Labels, got.Labels)
			}
			reconcile(prev)
			wantUpdates := 0
			if wantUpdate {
				wantUpdates = 1
			}
			require.Equal(t, wantUpdates, updates)
			reconcile(desired.DeepCopy())
			require.Equal(t, wantUpdates, updates, "repeated reconcile must not write")
		})
	}
	nginx, other := ptr.To("nginx"), ptr.To("other")
	f("host removal", ingress("example.com", nil), ingress("", nil), ingress("example.com", nil), ingress("", nil), true)
	f("explicit class removal", ingress("", nginx), ingress("", nil), ingress("", nginx), ingress("", nil), true)
	f("host and class removal", ingress("example.com", nginx), ingress("", nil), ingress("example.com", nginx), ingress("", nil), true)
	f("class replacement", ingress("", nginx), ingress("", other), ingress("", nginx), ingress("", other), true)
	f("explicit empty class", ingress("", nil), ingress("", ptr.To("")), ingress("", nginx), ingress("", ptr.To("")), true)
	f("default class unchanged", ingress("", nil), ingress("", nil), ingress("", nginx), ingress("", nginx), false)
	f("unknown previous class", nil, ingress("", nil), ingress("", nginx), ingress("", nginx), false)
	f("default class with host removal", ingress("example.com", nil), ingress("", nil), ingress("example.com", nginx), ingress("", nginx), true)
	f("no default class", ingress("", nil), ingress("", nil), ingress("", nil), ingress("", nil), false)

	desired := ingress("", nil)
	desired.Labels = map[string]string{"test": "new"}
	f("default class with metadata update", ingress("", nil), desired, ingress("", nginx), ingress("", nginx), true)
}
