package k8stools

import (
	"context"
	"fmt"
	"time"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// HandleDeployUpdate performs an update or create operator for deployment and waits until it's replicas is ready
func HandleDeployUpdate(ctx context.Context, rclient client.Client, newDeploy *appsv1.Deployment, waitDeadline time.Duration) error {
	var currentDeploy appsv1.Deployment
	err := rclient.Get(ctx, types.NamespacedName{Name: newDeploy.Name, Namespace: newDeploy.Namespace}, &currentDeploy)
	if err != nil {
		if errors.IsNotFound(err) {
			if err := rclient.Create(ctx, newDeploy); err != nil {
				return fmt.Errorf("cannot create new deployment for app: %s, err: %w", newDeploy.Name, err)
			}
			return waitDeploymentReady(ctx, rclient, newDeploy, waitDeadline)
		}
		return fmt.Errorf("cannot get deployment for app: %s err: %w", newDeploy.Name, err)
	}
	newDeploy.Spec.Template.Annotations = MergeAnnotations(currentDeploy.Spec.Template.Annotations, newDeploy.Spec.Template.Annotations)
	newDeploy.Finalizers = victoriametricsv1beta1.MergeFinalizers(&currentDeploy, victoriametricsv1beta1.FinalizerName)
	newDeploy.Status = currentDeploy.Status
	newDeploy.Annotations = MergeAnnotations(currentDeploy.Annotations, newDeploy.Annotations)

	if err := rclient.Update(ctx, newDeploy); err != nil {
		return fmt.Errorf("cannot update deployment for app: %s, err: %w", newDeploy.Name, err)
	}

	return waitDeploymentReady(ctx, rclient, newDeploy, waitDeadline)
}

// waitDeploymentReady waits until deployment's replicaSet rollouts and all new pods is ready
func waitDeploymentReady(ctx context.Context, rclient client.Client, dep *appsv1.Deployment, deadline time.Duration) error {
	time.Sleep(time.Second * 2)
	return wait.PollImmediateWithContext(ctx, time.Second*5, deadline, func(ctx context.Context) (done bool, err error) {
		var actualDeploy appsv1.Deployment
		if err := rclient.Get(ctx, types.NamespacedName{Namespace: dep.Namespace, Name: dep.Name}, &actualDeploy); err != nil {
			return false, fmt.Errorf("cannot fetch actual deployment state: %w", err)
		}
		for _, cond := range actualDeploy.Status.Conditions {
			if cond.Type == appsv1.DeploymentProgressing {
				// https://kubernetes.io/docs/concepts/workloads/controllers/deployment/#complete-deployment
				// Completed status for deployment
				if cond.Reason == "NewReplicaSetAvailable" && cond.Status == "True" {
					return true, nil
				}
				return false, nil
			}
		}
		return false, nil
	})
}
