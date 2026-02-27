package reconcile

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestHTTPRouteReconcile(t *testing.T) {
	type opts struct {
		new, prev         *gwapiv1.HTTPRoute
		predefinedObjects []runtime.Object
		actions           []k8stools.ClientAction
	}
	getHTTPRoute := func(fns ...func(r *gwapiv1.HTTPRoute)) *gwapiv1.HTTPRoute {
		r := &gwapiv1.HTTPRoute{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-route",
				Namespace: "default",
			},
			Spec: gwapiv1.HTTPRouteSpec{
				CommonRouteSpec: gwapiv1.CommonRouteSpec{
					ParentRefs: []gwapiv1.ParentReference{
						{
							Name: "test-gateway",
						},
					},
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
		assert.NoError(t, HTTPRoute(ctx, cl, o.new, o.prev, nil))
		assert.Equal(t, o.actions, cl.Actions)
	}

	nn := types.NamespacedName{Name: "test-route", Namespace: "default"}

	// create
	f(opts{
		new: getHTTPRoute(),
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "HTTPRoute", Resource: nn},
			{Verb: "Create", Kind: "HTTPRoute", Resource: nn},
		},
	})

	// no updates
	f(opts{
		new:  getHTTPRoute(),
		prev: getHTTPRoute(),
		predefinedObjects: []runtime.Object{
			getHTTPRoute(),
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "HTTPRoute", Resource: nn},
		},
	})

	// no update on status change
	f(opts{
		new:  getHTTPRoute(),
		prev: getHTTPRoute(),
		predefinedObjects: []runtime.Object{
			getHTTPRoute(func(r *gwapiv1.HTTPRoute) {
				r.Status.Parents = []gwapiv1.RouteParentStatus{
					{
						ParentRef: gwapiv1.ParentReference{
							Name: "test-gateway",
						},
						Conditions: []metav1.Condition{
							{
								Type:   "Accepted",
								Status: metav1.ConditionTrue,
							},
						},
					},
				}
			}),
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "HTTPRoute", Resource: nn},
		},
	})

	// update spec
	f(opts{
		new: getHTTPRoute(func(r *gwapiv1.HTTPRoute) {
			r.Spec.ParentRefs = append(r.Spec.ParentRefs, gwapiv1.ParentReference{Name: "another-gateway"})
		}),
		prev: getHTTPRoute(),
		predefinedObjects: []runtime.Object{
			getHTTPRoute(),
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "HTTPRoute", Resource: nn},
			{Verb: "Update", Kind: "HTTPRoute", Resource: nn},
		},
	})
}
