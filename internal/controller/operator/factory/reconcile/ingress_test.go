package reconcile

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

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
		assert.NoError(t, Ingress(ctx, cl, o.new, o.prev, nil))
		assert.Equal(t, o.actions, cl.Actions)
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
