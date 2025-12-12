package build

import (
	"fmt"
	"strconv"

	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// PodDisruptionBudget creates object for given CRD
func PodDisruptionBudget(cr builderOpts, spec *vmv1beta1.EmbeddedPodDisruptionBudgetSpec) *policyv1.PodDisruptionBudget {
	return PodDisruptionBudgetSharded(cr, spec, nil)
}

// PodDisruptionBudgetSharded creates object for given CRD and shard num
func PodDisruptionBudgetSharded(cr builderOpts, spec *vmv1beta1.EmbeddedPodDisruptionBudgetSpec, num *int) *policyv1.PodDisruptionBudget {
	pdb := policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(),
			Annotations:     cr.FinalAnnotations(),
			Labels:          cr.FinalLabels(),
			OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
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
	if num != nil {
		pdb.Name = fmt.Sprintf("%s-%d", pdb.Name, *num)
		pdb.Spec.Selector.MatchLabels[shardLabelName] = strconv.Itoa(*num)
	}
	if len(spec.UnhealthyPodEvictionPolicy) > 0 {
		p := policyv1.UnhealthyPodEvictionPolicyType(spec.UnhealthyPodEvictionPolicy)
		pdb.Spec.UnhealthyPodEvictionPolicy = &p
	}
	return &pdb
}
