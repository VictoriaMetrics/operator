package build

import (
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"

	v2 "k8s.io/api/autoscaling/v2"
	"k8s.io/api/autoscaling/v2beta2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// HPA creates HorizontalPodAutoscaler object
func HPA(targetRef v2beta2.CrossVersionObjectReference, spec *vmv1beta1.EmbeddedHPA, or []metav1.OwnerReference, lbls map[string]string, namespace string) *v2.HorizontalPodAutoscaler {
	return &v2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      targetRef.Name,
			Namespace: namespace,
			// TODO: @f41gh7 add annotations
			Labels:          lbls,
			OwnerReferences: or,
		},
		Spec: v2.HorizontalPodAutoscalerSpec{
			MaxReplicas:    spec.MaxReplicas,
			MinReplicas:    spec.MinReplicas,
			ScaleTargetRef: v2.CrossVersionObjectReference(targetRef),
			// TODO: @f41gh7 deprecate v2beta2 and use autoscalingv2 only
			Metrics:  *k8stools.MustConvertObjectVersionsJSON[[]v2beta2.MetricSpec, []v2.MetricSpec](&spec.Metrics, "v2beta/autoscaling/metricsSpec"),
			Behavior: k8stools.MustConvertObjectVersionsJSON[v2beta2.HorizontalPodAutoscalerBehavior, v2.HorizontalPodAutoscalerBehavior](spec.Behaviour, "v2beta/autoscaling/behaviorSpec"),
		},
	}
}
