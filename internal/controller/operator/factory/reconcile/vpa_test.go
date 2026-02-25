package reconcile

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestVPAReconcile(t *testing.T) {
	type opts struct {
		new, prev         *vpav1.VerticalPodAutoscaler
		predefinedObjects []runtime.Object
		actions           []k8stools.ClientAction
	}
	getVPA := func(fns ...func(v *vpav1.VerticalPodAutoscaler)) *vpav1.VerticalPodAutoscaler {
		updateMode := vpav1.UpdateModeAuto
		vpa := &vpav1.VerticalPodAutoscaler{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-vpa",
				Namespace:  "default",
				Finalizers: []string{vmv1beta1.FinalizerName},
			},
			Spec: vpav1.VerticalPodAutoscalerSpec{
				TargetRef: &autoscalingv1.CrossVersionObjectReference{
					Kind:       "StatefulSet",
					Name:       "test-sts",
					APIVersion: "apps/v1",
				},
				UpdatePolicy: &vpav1.PodUpdatePolicy{
					UpdateMode: &updateMode,
				},
			},
		}
		for _, fn := range fns {
			fn(vpa)
		}
		return vpa
	}

	f := func(o opts) {
		t.Helper()
		ctx := context.Background()
		cl := k8stools.GetTestClientWithActions(o.predefinedObjects)
		assert.NoError(t, VPA(ctx, cl, o.new, o.prev, nil))
		assert.Equal(t, o.actions, cl.Actions)
	}

	nn := types.NamespacedName{Name: "test-vpa", Namespace: "default"}

	// create
	f(opts{
		new: getVPA(),
		actions: []k8stools.ClientAction{
			{Verb: "Get", Resource: nn},
			{Verb: "Create", Resource: nn},
		},
	})

	// no updates
	f(opts{
		new:  getVPA(),
		prev: getVPA(),
		predefinedObjects: []runtime.Object{
			getVPA(),
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Resource: nn},
		},
	})

	// update spec
	f(opts{
		new: getVPA(func(v *vpav1.VerticalPodAutoscaler) {
			mode := vpav1.UpdateModeOff
			v.Spec.UpdatePolicy.UpdateMode = &mode
		}),
		prev: getVPA(),
		predefinedObjects: []runtime.Object{
			getVPA(),
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Resource: nn},
			{Verb: "Update", Resource: nn},
		},
	})
}
