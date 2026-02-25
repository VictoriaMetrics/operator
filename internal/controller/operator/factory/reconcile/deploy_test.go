package reconcile

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestDeployReconcile(t *testing.T) {
	type opts struct {
		new, prev         *appsv1.Deployment
		predefinedObjects []runtime.Object
		actions           []k8stools.ClientAction
	}
	getDeploy := func(fns ...func(d *appsv1.Deployment)) *appsv1.Deployment {
		d := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-1",
				Namespace: "default",
			},
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"label": "value",
					},
				},
				Replicas: ptr.To[int32](1),
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{"label": "value"},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:            "vmalert",
								ImagePullPolicy: "IfNowPresent",
								Image:           "some-image:tag",
							},
						},
					},
				},
			},
			Status: appsv1.DeploymentStatus{
				Conditions: []appsv1.DeploymentCondition{
					{
						Type:   appsv1.DeploymentProgressing,
						Reason: "NewReplicaSetAvailable",
						Status: "True",
					},
				},
				ReadyReplicas:   1,
				UpdatedReplicas: 1,
				Replicas:        1,
			},
		}
		for _, fn := range fns {
			fn(d)
		}
		return d
	}
	f := func(o opts) {
		t.Helper()
		ctx := context.Background()
		cl := k8stools.GetTestClientWithActions(o.predefinedObjects)
		assert.NoError(t, Deployment(ctx, cl, o.new, o.prev, false, nil))
		assert.Equal(t, o.actions, cl.Actions)
	}

	nn := types.NamespacedName{Name: "test-1", Namespace: "default"}

	// create deployment
	f(opts{
		new: getDeploy(),
		actions: []k8stools.ClientAction{
			{Verb: "Get", Resource: nn},
			{Verb: "Create", Resource: nn},
			{Verb: "Get", Resource: nn},
		},
	})

	// no updates
	f(opts{
		new:  getDeploy(),
		prev: getDeploy(),
		predefinedObjects: []runtime.Object{
			getDeploy(func(d *appsv1.Deployment) {
				d.Finalizers = []string{vmv1beta1.FinalizerName}
			}),
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Resource: nn},
			{Verb: "Get", Resource: nn},
		},
	})

	// update spec
	f(opts{
		new: getDeploy(func(d *appsv1.Deployment) {
			d.Spec.Template.Annotations = map[string]string{"new-annotation": "value"}
		}),
		prev: getDeploy(),
		predefinedObjects: []runtime.Object{
			getDeploy(),
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Resource: nn},
			{Verb: "Update", Resource: nn},
			{Verb: "Get", Resource: nn},
		},
	})

	// remove template annotations
	f(opts{
		new:  getDeploy(),
		prev: getDeploy(),
		predefinedObjects: []runtime.Object{
			getDeploy(func(d *appsv1.Deployment) {
				d.Spec.Template.Annotations = map[string]string{"new-annotation": "value"}
			}),
		},
		hasHPA: true,
		actions: []k8stools.ClientAction{
			{Verb: "Get", Resource: nn},
			{Verb: "Get", Resource: nn},
		},
	})
}
