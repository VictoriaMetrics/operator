package reconcile

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	v2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

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
		assert.NoError(t, HPA(ctx, cl, o.new, o.prev, nil))
		assert.Equal(t, o.actions, cl.Actions)
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
