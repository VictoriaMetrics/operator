package build

import (
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// HPA creates HorizontalPodAutoscaler object
func HPA(opts deployOpts, targetRef autoscalingv2.CrossVersionObjectReference, spec *vmv1beta1.EmbeddedHPA) *autoscalingv2.HorizontalPodAutoscaler {
	return &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:            targetRef.Name,
			Namespace:       opts.GetNamespace(),
			Annotations:     opts.AnnotationsFiltered(),
			Labels:          opts.AllLabels(),
			OwnerReferences: opts.AsOwner(),
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MaxReplicas:    spec.MaxReplicas,
			MinReplicas:    spec.MinReplicas,
			ScaleTargetRef: targetRef,
			Metrics:        spec.Metrics,
			Behavior:       spec.Behaviour,
		},
	}
}
