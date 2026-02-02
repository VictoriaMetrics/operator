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

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestDeployReconcile(t *testing.T) {
	type opts struct {
		new, prev         *appsv1.Deployment
		predefinedObjects []runtime.Object
		validate          func(*k8stools.TestClientWithStatsTrack, *appsv1.Deployment)
		wantErr           bool
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
				ReadyReplicas:     1,
				UpdatedReplicas:   1,
				AvailableReplicas: 1,
				Replicas:          1,
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
		rclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		err := Deployment(ctx, rclient, o.new, o.prev, false, nil)
		if err != nil {
			t.Fatalf("failed to wait deployment created: %s", err)
		}
		if o.wantErr {
			assert.Error(t, err)
			return
		} else {
			assert.NoError(t, err)
		}
		nsn := types.NamespacedName{
			Name:      o.new.Name,
			Namespace: o.new.Namespace,
		}
		var got appsv1.Deployment
		assert.NoError(t, rclient.Get(ctx, nsn, &got))
		if o.validate != nil {
			o.validate(rclient, &got)
		}
	}

	// create deployment
	f(opts{
		new: getDeploy(),
		validate: func(rclient *k8stools.TestClientWithStatsTrack, d *appsv1.Deployment) {
			assert.Equal(t, 2, rclient.GetCalls.Count(d))
			assert.Equal(t, 1, rclient.CreateCalls.Count(d))
			assert.Equal(t, 0, rclient.UpdateCalls.Count(d))
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
		validate: func(rclient *k8stools.TestClientWithStatsTrack, d *appsv1.Deployment) {
			assert.Equal(t, 3, rclient.GetCalls.Count(d))
			assert.Equal(t, 0, rclient.CreateCalls.Count(d))
			assert.Equal(t, 0, rclient.UpdateCalls.Count(d))
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
		validate: func(rclient *k8stools.TestClientWithStatsTrack, d *appsv1.Deployment) {
			assert.Equal(t, 0, rclient.CreateCalls.Count(d))
			assert.Equal(t, 1, rclient.UpdateCalls.Count(d))
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
		validate: func(rclient *k8stools.TestClientWithStatsTrack, d *appsv1.Deployment) {
			assert.Equal(t, 0, rclient.CreateCalls.Count(d))
			assert.Equal(t, 1, rclient.UpdateCalls.Count(d))
		},
	})
}
