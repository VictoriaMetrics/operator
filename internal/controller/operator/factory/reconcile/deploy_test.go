package reconcile

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/ptr"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestDeployOk(t *testing.T) {
	f := func(dep *appsv1.Deployment) {
		t.Helper()
		ctx := context.Background()
		rclient := k8stools.GetTestClientWithObjects(nil)
		clientStats := rclient.(*k8stools.TestClientWithStatsTrack)

		waitTimeout := 5 * time.Second
		prevDeploy := dep.DeepCopy()
		createErr := make(chan error)
		go func() {
			err := Deployment(ctx, rclient, dep, nil, false)
			createErr <- err
		}()
		reloadDep := func() {
			t.Helper()
			if err := rclient.Get(ctx, types.NamespacedName{Name: dep.Name, Namespace: dep.Namespace}, dep); err != nil {
				t.Fatalf("cannot reload created deployment: %s", err)
			}
		}

		err := wait.PollUntilContextTimeout(ctx, time.Millisecond*50,
			waitTimeout, true, func(ctx context.Context) (done bool, err error) {
				var createdDep appsv1.Deployment
				if err := rclient.Get(ctx, types.NamespacedName{Name: dep.Name, Namespace: dep.Namespace}, &createdDep); err != nil {
					if k8serrors.IsNotFound(err) {
						return false, nil
					}
					return false, err
				}
				createdDep.Status.Conditions = []appsv1.DeploymentCondition{
					{
						Type:   appsv1.DeploymentProgressing,
						Reason: "NewReplicaSetAvailable",
						Status: "True",
					},
				}
				createdDep.Status.ReadyReplicas = 1
				createdDep.Status.UpdatedReplicas = 1
				createdDep.Status.AvailableReplicas = 1
				createdDep.Status.Replicas = 1
				if err := rclient.Status().Update(ctx, &createdDep); err != nil {
					return false, err
				}
				return true, nil
			})
		if err != nil {
			t.Fatalf("failed to wait deployment created: %s", err)
		}

		err = <-createErr
		if err != nil {
			t.Fatalf("failed to create deploy: %s", err)
		}
		// expect 1 create
		assert.Equal(t, 1, clientStats.CreateCalls.Count(dep))
		// expect 0 update
		if err := Deployment(ctx, rclient, dep, prevDeploy, false); err != nil {
			t.Fatalf("failed to update created deploy: %s", err)
		}
		assert.Equal(t, 1, clientStats.CreateCalls.Count(dep))
		assert.Equal(t, 0, clientStats.UpdateCalls.Count(dep))

		// expect 1 UpdateCalls
		reloadDep()
		dep.Status.AvailableReplicas = 10
		dep.Status.UpdatedReplicas = 10
		if err = rclient.Status().Update(ctx, dep); err != nil {
			t.Fatalf("cannot update deployment status: %s", err)
		}

		dep.Spec.Replicas = ptr.To[int32](10)
		dep.Spec.Template.Annotations = map[string]string{"new-annotation": "value"}
		if err := Deployment(ctx, rclient, dep, prevDeploy, false); err != nil {
			t.Fatalf("expect 1 failed to update created deploy: %s", err)
		}
		assert.Equal(t, 1, clientStats.CreateCalls.Count(dep))
		assert.Equal(t, 1, clientStats.UpdateCalls.Count(dep))

		// expected still same 1 update
		reloadDep()
		if err := Deployment(ctx, rclient, dep, prevDeploy, false); err != nil {
			t.Fatalf("expect still 1 failed to update created deploy: %s", err)
		}
		assert.Equal(t, 1, clientStats.CreateCalls.Count(dep))
		assert.Equal(t, 1, clientStats.UpdateCalls.Count(dep))

		// expected 2 updates
		prevDeploy.Spec.Template.Annotations = dep.Spec.Template.Annotations
		dep.Spec.Template.Annotations = nil

		if err := Deployment(ctx, rclient, dep, prevDeploy, false); err != nil {
			t.Fatalf("expect 2 failed to update deploy: %s", err)
		}
		assert.Equal(t, 1, clientStats.CreateCalls.Count(dep))
		assert.Equal(t, 2, clientStats.UpdateCalls.Count(dep))
	}

	f(&appsv1.Deployment{
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
	})
}
