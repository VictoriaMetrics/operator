package build

import (
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// NetworkPolicy creates a NetworkPolicy for the given CRD.
// The PodSelector is set to the CR's selector labels so it covers all pods owned by the CR.
// PolicyTypes are inferred from the presence of Ingress/Egress rules in spec.
func NetworkPolicy(cr builderOpts, spec *vmv1beta1.EmbeddedNetworkPolicy) *networkingv1.NetworkPolicy {
	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(),
			Namespace:       cr.GetNamespace(),
			Annotations:     cr.FinalAnnotations(),
			Labels:          cr.FinalLabels(),
			OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: cr.SelectorLabels(),
			},
		},
	}
	if len(spec.Ingress) > 0 {
		np.Spec.PolicyTypes = append(np.Spec.PolicyTypes, networkingv1.PolicyTypeIngress)
		np.Spec.Ingress = spec.Ingress
	}
	if len(spec.Egress) > 0 {
		np.Spec.PolicyTypes = append(np.Spec.PolicyTypes, networkingv1.PolicyTypeEgress)
		np.Spec.Egress = spec.Egress
	}
	return np
}
