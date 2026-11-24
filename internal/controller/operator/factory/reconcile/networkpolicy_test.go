package reconcile

import (
	"context"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
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
		setNetworkPolicyDefaults(cl.Scheme())
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

func TestNetworkPolicyDefaults(t *testing.T) {
	f := func(egress bool) {
		t.Helper()
		ctx := context.Background()
		desired := &networkingv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{Name: "test-np", Namespace: "default"},
			Spec: networkingv1.NetworkPolicySpec{
				Ingress: []networkingv1.NetworkPolicyIngressRule{{Ports: []networkingv1.NetworkPolicyPort{{}}}},
			},
		}
		existing := desired.DeepCopy()
		existing.Spec.PolicyTypes = []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}
		existing.Spec.Ingress[0].Ports[0].Protocol = ptr.To(corev1.ProtocolTCP)
		if egress {
			desired.Spec.Egress = []networkingv1.NetworkPolicyEgressRule{{Ports: []networkingv1.NetworkPolicyPort{{}}}}
			existing.Spec.Egress = []networkingv1.NetworkPolicyEgressRule{{Ports: []networkingv1.NetworkPolicyPort{{Protocol: ptr.To(corev1.ProtocolTCP)}}}}
			existing.Spec.PolicyTypes = append(existing.Spec.PolicyTypes, networkingv1.PolicyTypeEgress)
		}
		cl := k8stools.GetTestClientWithActions([]runtime.Object{existing})
		setNetworkPolicyDefaults(cl.Scheme())
		require.NoError(t, NetworkPolicy(ctx, cl, desired, nil, nil))
		assert.Equal(t, []k8stools.ClientAction{
			{Verb: "Get", Kind: "NetworkPolicy", Resource: types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}},
		}, cl.Actions)
	}

	f(false) // ingress only
	f(true)  // ingress and egress
}

func TestNetworkPolicyClearRules(t *testing.T) {
	f := func(emptySlice, ingressOnly bool) {
		t.Helper()
		ctx := context.Background()
		prev := &networkingv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{Name: "test-np", Namespace: "default"},
			Spec: networkingv1.NetworkPolicySpec{
				Ingress:     []networkingv1.NetworkPolicyIngressRule{{}},
				Egress:      []networkingv1.NetworkPolicyEgressRule{{}},
				PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress},
			},
		}
		if ingressOnly {
			prev.Spec.Egress = nil
			prev.Spec.PolicyTypes = []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}
		}
		desired := prev.DeepCopy()
		desired.Spec = networkingv1.NetworkPolicySpec{}
		if emptySlice {
			desired.Spec.Ingress = []networkingv1.NetworkPolicyIngressRule{}
			desired.Spec.Egress = []networkingv1.NetworkPolicyEgressRule{}
			desired.Spec.PolicyTypes = []networkingv1.PolicyType{}
		}
		cl := k8stools.GetTestClientWithActions([]runtime.Object{prev})
		setNetworkPolicyDefaults(cl.Scheme())
		nn := types.NamespacedName{Name: prev.Name, Namespace: prev.Namespace}

		require.NoError(t, NetworkPolicy(ctx, cl, desired.DeepCopy(), prev, nil))
		require.Equal(t, []k8stools.ClientAction{
			{Verb: "Get", Kind: "NetworkPolicy", Resource: nn},
			{Verb: "Update", Kind: "NetworkPolicy", Resource: nn},
		}, cl.Actions)
		var got networkingv1.NetworkPolicy
		require.NoError(t, cl.Get(ctx, nn, &got))
		assert.Empty(t, got.Spec.Ingress)
		assert.Empty(t, got.Spec.Egress)
		assert.Equal(t, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}, got.Spec.PolicyTypes)

		cl.Actions = nil
		require.NoError(t, NetworkPolicy(ctx, cl, desired.DeepCopy(), desired, nil))
		assert.Equal(t, []k8stools.ClientAction{
			{Verb: "Get", Kind: "NetworkPolicy", Resource: nn},
		}, cl.Actions)
	}

	f(false, false) // nil rules
	f(true, false)  // empty slices
	f(false, true)  // ingress only
}

func setNetworkPolicyDefaults(scheme *runtime.Scheme) {
	defaults := runtime.NewScheme()
	build.AddDefaults(defaults)
	scheme.AddTypeDefaultingFunc(&networkingv1.NetworkPolicy{}, func(obj any) {
		defaults.Default(obj.(*networkingv1.NetworkPolicy))
	})
}
