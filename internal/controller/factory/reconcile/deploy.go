package reconcile

import (
	"context"
	"fmt"
	"time"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/factory/finalize"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Deployment performs an update or create operator for deployment and waits until it's replicas is ready
func Deployment(ctx context.Context, rclient client.Client, newDeploy *appsv1.Deployment, waitDeadline time.Duration, hasHPA bool) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
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
		if err := finalize.FreeIfNeeded(ctx, rclient, &currentDeploy); err != nil {
			return err
		}
		if hasHPA {
			newDeploy.Spec.Replicas = currentDeploy.Spec.Replicas
		}
		newDeploy.Spec.Template.Annotations = labels.Merge(currentDeploy.Spec.Template.Annotations, newDeploy.Spec.Template.Annotations)
		newDeploy.Finalizers = vmv1beta1.MergeFinalizers(&currentDeploy, vmv1beta1.FinalizerName)
		newDeploy.Status = currentDeploy.Status
		newDeploy.Annotations = labels.Merge(currentDeploy.Annotations, newDeploy.Annotations)

		if err := rclient.Update(ctx, newDeploy); err != nil {
			return fmt.Errorf("cannot update deployment for app: %s, err: %w", newDeploy.Name, err)
		}

		return waitDeploymentReady(ctx, rclient, newDeploy, waitDeadline)
	})
}

// waitDeploymentReady waits until deployment's replicaSet rollouts and all new pods is ready
func waitDeploymentReady(ctx context.Context, rclient client.Client, dep *appsv1.Deployment, deadline time.Duration) error {
	err := wait.PollWithContext(ctx, time.Second*5, deadline, func(ctx context.Context) (done bool, err error) {
		var actualDeploy appsv1.Deployment
		if err := rclient.Get(ctx, types.NamespacedName{Namespace: dep.Namespace, Name: dep.Name}, &actualDeploy); err != nil {
			return false, fmt.Errorf("cannot fetch actual deployment state: %w", err)
		}
		for _, cond := range actualDeploy.Status.Conditions {
			if cond.Type == appsv1.DeploymentProgressing {
				// https://kubernetes.io/docs/concepts/workloads/internal/controller/deployment/#complete-deployment
				// Completed status for deployment
				if cond.Reason == "NewReplicaSetAvailable" && cond.Status == "True" {
					return true, nil
				}
				return false, nil
			}
		}
		return false, nil
	})
	if err != nil {
		return reportFirstNotReadyPodOnError(ctx, rclient, fmt.Errorf("cannot wait for deployment to become ready: %w", err), dep.Namespace, labels.SelectorFromSet(dep.Spec.Selector.MatchLabels), dep.Spec.MinReadySeconds)
	}
	return nil
}

func reportFirstNotReadyPodOnError(ctx context.Context, rclient client.Client, origin error, ns string, selector labels.Selector, minReadySeconds int32) error {
	// list pods and join statuses
	var podList corev1.PodList
	if err := rclient.List(ctx, &podList, &client.ListOptions{
		LabelSelector: selector,
		Namespace:     ns,
	}); err != nil {
		return fmt.Errorf("cannot list pods for selector=%q: %w", selector.String(), err)
	}
	for _, dp := range podList.Items {
		if PodIsReady(&dp, minReadySeconds) {
			continue
		}
		return podStatusesToError(origin, &dp)
	}
	return fmt.Errorf("cannot find any pod for selector=%q, check kubernetes events, origin err: %w", selector.String(), origin)
}
