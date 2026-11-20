package reconcile

import (
	"context"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestHPADefaultsAndRemoval(t *testing.T) {
	f := func(previous, desired v2.HorizontalPodAutoscalerSpec, update bool) {
		t.Helper()
		nn := types.NamespacedName{Name: "test-hpa", Namespace: "default"}
		prev := &v2.HorizontalPodAutoscaler{ObjectMeta: metav1.ObjectMeta{Name: nn.Name, Namespace: nn.Namespace}, Spec: previous}
		next := prev.DeepCopy()
		next.Spec = desired
		cl := k8stools.GetTestClientWithActions([]runtime.Object{prev})
		defaults := runtime.NewScheme()
		build.AddDefaults(defaults)
		cl.Scheme().AddTypeDefaultingFunc(&v2.HorizontalPodAutoscaler{}, func(obj any) {
			defaults.Default(obj.(*v2.HorizontalPodAutoscaler))
		})
		synctest.Test(t, func(t *testing.T) {
			ctx := context.Background()
			require.NoError(t, HPA(ctx, cl, next.DeepCopy(), prev, nil))
			actions := []k8stools.ClientAction{{Verb: "Get", Kind: "HorizontalPodAutoscaler", Resource: nn}}
			if update {
				actions = append(actions, k8stools.ClientAction{Verb: "Update", Kind: "HorizontalPodAutoscaler", Resource: nn})
			}
			assert.Equal(t, actions, cl.Actions)
			var got v2.HorizontalPodAutoscaler
			require.NoError(t, cl.Get(ctx, nn, &got))
			defaults.Default(&got)
			defaults.Default(next)
			assert.Equal(t, next.Spec, got.Spec)

			cl.Actions = nil
			require.NoError(t, HPA(ctx, cl, next.DeepCopy(), next, nil))
			assert.Equal(t, []k8stools.ClientAction{{Verb: "Get", Kind: "HorizontalPodAutoscaler", Resource: nn}}, cl.Actions)
		})
	}

	omitted := v2.HorizontalPodAutoscalerSpec{
		ScaleTargetRef: v2.CrossVersionObjectReference{
			Kind: "VMAgent", Name: "test", APIVersion: "operator.victoriametrics.com/v1beta1",
		},
		MaxReplicas: 3,
	}
	explicit := *omitted.DeepCopy()
	explicit.MinReplicas = ptr.To[int32](1)
	f(explicit, omitted, false)
	explicit.MinReplicas = ptr.To[int32](2)
	f(explicit, omitted, true)
	f(omitted, explicit, true)
	f(explicit, explicit, false)

	withMetric := *explicit.DeepCopy()
	withMetric.Metrics = []v2.MetricSpec{{
		Type: v2.ResourceMetricSourceType,
		Resource: &v2.ResourceMetricSource{
			Name: corev1.ResourceCPU,
			Target: v2.MetricTarget{
				Type: v2.UtilizationMetricType, AverageUtilization: ptr.To[int32](60),
			},
		},
	}}
	f(withMetric, explicit, true)
	withBehavior := *explicit.DeepCopy()
	withBehavior.Behavior = &v2.HorizontalPodAutoscalerBehavior{
		ScaleUp: &v2.HPAScalingRules{SelectPolicy: ptr.To(v2.DisabledPolicySelect)},
	}
	f(withBehavior, explicit, true)
	emptyBehavior := *explicit.DeepCopy()
	emptyBehavior.Behavior = &v2.HorizontalPodAutoscalerBehavior{}
	f(withBehavior, emptyBehavior, true)
	f(emptyBehavior, emptyBehavior, false)

	defaults := runtime.NewScheme()
	build.AddDefaults(defaults)
	apiDefaulted := &v2.HorizontalPodAutoscaler{Spec: omitted}
	defaults.Default(apiDefaulted)
	f(apiDefaulted.Spec, omitted, false)
}

func TestHPAReconcile(t *testing.T) {
	type opts struct {
		new, prev         *v2.HorizontalPodAutoscaler
		predefinedObjects []runtime.Object
		actions           []k8stools.ClientAction
	}
	getHPA := func(fns ...func(h *v2.HorizontalPodAutoscaler)) *v2.HorizontalPodAutoscaler {
		h := &v2.HorizontalPodAutoscaler{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-hpa",
				Namespace: "default",
			},
			Spec: v2.HorizontalPodAutoscalerSpec{
				ScaleTargetRef: v2.CrossVersionObjectReference{
					Kind:       "Deployment",
					Name:       "test-deploy",
					APIVersion: "apps/v1",
				},
				MinReplicas: ptr.To[int32](1),
				MaxReplicas: 5,
			},
		}
		for _, fn := range fns {
			fn(h)
		}
		return h
	}

	f := func(o opts) {
		t.Helper()
		ctx := context.Background()
		cl := k8stools.GetTestClientWithActions(o.predefinedObjects)
		synctest.Test(t, func(t *testing.T) {
			assert.NoError(t, HPA(ctx, cl, o.new, o.prev, nil))
			assert.Equal(t, o.actions, cl.Actions)
		})
	}

	nn := types.NamespacedName{Name: "test-hpa", Namespace: "default"}

	// create hpa
	f(opts{
		new: getHPA(),
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "HorizontalPodAutoscaler", Resource: nn},
			{Verb: "Create", Kind: "HorizontalPodAutoscaler", Resource: nn},
		},
	})

	// no updates
	f(opts{
		new:  getHPA(),
		prev: getHPA(),
		predefinedObjects: []runtime.Object{
			getHPA(),
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "HorizontalPodAutoscaler", Resource: nn},
		},
	})

	// no update on status change
	f(opts{
		new:  getHPA(),
		prev: getHPA(),
		predefinedObjects: []runtime.Object{
			getHPA(func(h *v2.HorizontalPodAutoscaler) {
				h.Status.CurrentReplicas = 1
			}),
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "HorizontalPodAutoscaler", Resource: nn},
		},
	})

	// update spec
	f(opts{
		new: getHPA(func(h *v2.HorizontalPodAutoscaler) {
			h.Spec.MaxReplicas = 10
		}),
		prev: getHPA(),
		predefinedObjects: []runtime.Object{
			getHPA(),
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "HorizontalPodAutoscaler", Resource: nn},
			{Verb: "Update", Kind: "HorizontalPodAutoscaler", Resource: nn},
		},
	})
}
