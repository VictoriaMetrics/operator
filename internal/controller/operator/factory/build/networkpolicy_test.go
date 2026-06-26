package build

import (
	"testing"

	"github.com/stretchr/testify/assert"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func TestNetworkPolicy(t *testing.T) {
	cr := &vmv1beta1.VMSingle{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
	}

	type opts struct {
		spec     *vmv1beta1.EmbeddedNetworkPolicy
		validate func(np *networkingv1.NetworkPolicy)
	}

	f := func(o opts) {
		t.Helper()
		np := NetworkPolicy(cr, o.spec)
		assert.Equal(t, "vmsingle-test", np.Name)
		assert.Equal(t, "default", np.Namespace)
		assert.Equal(t, cr.SelectorLabels(), np.Spec.PodSelector.MatchLabels)
		o.validate(np)
	}

	// no rules — no policy types
	f(opts{
		spec: &vmv1beta1.EmbeddedNetworkPolicy{},
		validate: func(np *networkingv1.NetworkPolicy) {
			assert.Empty(t, np.Spec.PolicyTypes)
			assert.Empty(t, np.Spec.Ingress)
			assert.Empty(t, np.Spec.Egress)
		},
	})

	// ingress only
	f(opts{
		spec: &vmv1beta1.EmbeddedNetworkPolicy{
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{Ports: []networkingv1.NetworkPolicyPort{{}}},
			},
		},
		validate: func(np *networkingv1.NetworkPolicy) {
			assert.Equal(t, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}, np.Spec.PolicyTypes)
			assert.Len(t, np.Spec.Ingress, 1)
			assert.Empty(t, np.Spec.Egress)
		},
	})

	// egress only
	f(opts{
		spec: &vmv1beta1.EmbeddedNetworkPolicy{
			Egress: []networkingv1.NetworkPolicyEgressRule{
				{Ports: []networkingv1.NetworkPolicyPort{{}}},
			},
		},
		validate: func(np *networkingv1.NetworkPolicy) {
			assert.Equal(t, []networkingv1.PolicyType{networkingv1.PolicyTypeEgress}, np.Spec.PolicyTypes)
			assert.Empty(t, np.Spec.Ingress)
			assert.Len(t, np.Spec.Egress, 1)
		},
	})

	// ingress and egress
	f(opts{
		spec: &vmv1beta1.EmbeddedNetworkPolicy{
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{Ports: []networkingv1.NetworkPolicyPort{{}}},
			},
			Egress: []networkingv1.NetworkPolicyEgressRule{
				{Ports: []networkingv1.NetworkPolicyPort{{}}},
			},
		},
		validate: func(np *networkingv1.NetworkPolicy) {
			assert.Equal(t, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress}, np.Spec.PolicyTypes)
			assert.Len(t, np.Spec.Ingress, 1)
			assert.Len(t, np.Spec.Egress, 1)
		},
	})
}
