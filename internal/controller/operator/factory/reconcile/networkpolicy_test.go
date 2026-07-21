package reconcile

import (
	"context"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestNetworkPolicyReconcile(t *testing.T) {
	type opts struct {
		new, prev         *networkingv1.NetworkPolicy
		predefinedObjects []runtime.Object
		actions           []k8stools.ClientAction
	}

	getNP := func(fns ...func(np *networkingv1.NetworkPolicy)) *networkingv1.NetworkPolicy {
		np := &networkingv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-np",
				Namespace: "default",
			},
			Spec: networkingv1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "test"},
				},
				PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
				Ingress: []networkingv1.NetworkPolicyIngressRule{
					{Ports: []networkingv1.NetworkPolicyPort{{}}},
				},
			},
		}
		for _, fn := range fns {
			fn(np)
		}
		return np
	}

	f := func(o opts) {
		t.Helper()
		ctx := context.Background()
		cl := k8stools.GetTestClientWithActions(o.predefinedObjects)
		synctest.Test(t, func(t *testing.T) {
			assert.NoError(t, NetworkPolicy(ctx, cl, o.new, o.prev, nil))
			assert.Equal(t, o.actions, cl.Actions)
		})
	}

	nn := types.NamespacedName{Name: "test-np", Namespace: "default"}

	// create
	f(opts{
		new: getNP(),
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "NetworkPolicy", Resource: nn},
			{Verb: "Create", Kind: "NetworkPolicy", Resource: nn},
		},
	})

	// no updates
	f(opts{
		new:               getNP(),
		prev:              getNP(),
		predefinedObjects: []runtime.Object{getNP()},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "NetworkPolicy", Resource: nn},
		},
	})

	// update spec — add egress
	f(opts{
		new: getNP(func(np *networkingv1.NetworkPolicy) {
			np.Spec.PolicyTypes = append(np.Spec.PolicyTypes, networkingv1.PolicyTypeEgress)
			np.Spec.Egress = []networkingv1.NetworkPolicyEgressRule{
				{Ports: []networkingv1.NetworkPolicyPort{{}}},
			}
		}),
		prev:              getNP(),
		predefinedObjects: []runtime.Object{getNP()},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "NetworkPolicy", Resource: nn},
			{Verb: "Update", Kind: "NetworkPolicy", Resource: nn},
		},
	})
}
