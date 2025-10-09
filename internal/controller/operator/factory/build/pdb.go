package build

import (
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// PodDisruptionBudget creates object for given CRD
func PodDisruptionBudget(cr builderOpts, spec *vmv1beta1.EmbeddedPodDisruptionBudgetSpec) *policyv1.PodDisruptionBudget {
	pdb := policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(),
			Annotations:     cr.AnnotationsFiltered(),
			Labels:          cr.AllLabels(),
			OwnerReferences: cr.AsOwner(),
			Namespace:       cr.GetNamespace(),
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MinAvailable:   spec.MinAvailable,
			MaxUnavailable: spec.MaxUnavailable,
			Selector: &metav1.LabelSelector{
				MatchLabels: spec.SelectorLabelsWithDefaults(cr.SelectorLabels()),
			},
		},
	}
	if len(spec.UnhealthyPodEvictionPolicy) > 0 {
		p := policyv1.UnhealthyPodEvictionPolicyType(spec.UnhealthyPodEvictionPolicy)
		pdb.Spec.UnhealthyPodEvictionPolicy = &p
	}
	return &pdb
}
