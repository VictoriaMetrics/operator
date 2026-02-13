package build

import (
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// VPA creates VerticalPodAutoscaler object
func VPA(opts builderOpts, targetRef autoscalingv1.CrossVersionObjectReference, spec *vmv1beta1.EmbeddedVPA) *vpav1.VerticalPodAutoscaler {
	return &vpav1.VerticalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:            targetRef.Name,
			Namespace:       opts.GetNamespace(),
			Annotations:     opts.FinalAnnotations(),
			Labels:          opts.FinalLabels(),
			OwnerReferences: []metav1.OwnerReference{opts.AsOwner()},
		},
		Spec: vpav1.VerticalPodAutoscalerSpec{
			TargetRef:      &targetRef,
			UpdatePolicy:   spec.UpdatePolicy,
			ResourcePolicy: spec.ResourcePolicy,
			Recommenders:   spec.Recommenders,
		},
	}
}
