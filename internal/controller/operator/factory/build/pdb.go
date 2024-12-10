package build

import (
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// PodDisruptionBudget creates object for given CRD
func PodDisruptionBudget(cr svcBuilderArgs, spec *vmv1beta1.EmbeddedPodDisruptionBudgetSpec) *policyv1.PodDisruptionBudget {
	return &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name: cr.PrefixedName(),
			// TODO: @f41gh7 add annotations
			Labels:          cr.AllLabels(),
			OwnerReferences: cr.AsOwner(),
			Namespace:       cr.GetNSName(),
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MinAvailable:   spec.MinAvailable,
			MaxUnavailable: spec.MaxUnavailable,
			Selector: &metav1.LabelSelector{
				MatchLabels: spec.SelectorLabelsWithDefaults(cr.SelectorLabels()),
			},
		},
	}
}
