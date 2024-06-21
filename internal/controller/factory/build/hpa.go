package build

import (
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/factory/k8stools"
	v2 "k8s.io/api/autoscaling/v2"
	"k8s.io/api/autoscaling/v2beta2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// HPA creates HorizontalPodAutoscaler object
func HPA(targetRef v2beta2.CrossVersionObjectReference, spec *vmv1beta1.EmbeddedHPA, or []metav1.OwnerReference, lbls map[string]string, namespace string) client.Object {
	return &v2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:            targetRef.Name,
			Namespace:       namespace,
			Labels:          lbls,
			OwnerReferences: or,
		},
		Spec: v2.HorizontalPodAutoscalerSpec{
			MaxReplicas:    spec.MaxReplicas,
			MinReplicas:    spec.MinReplicas,
			ScaleTargetRef: v2.CrossVersionObjectReference(targetRef),
			Metrics:        *k8stools.MustConvertObjectVersionsJSON[[]v2beta2.MetricSpec, []v2.MetricSpec](&spec.Metrics, "v2beta/autoscaling/metricsSpec"),
			Behavior:       k8stools.MustConvertObjectVersionsJSON[v2beta2.HorizontalPodAutoscalerBehavior, v2.HorizontalPodAutoscalerBehavior](spec.Behaviour, "v2beta/autoscaling/behaviorSpec"),
		},
	}
}
