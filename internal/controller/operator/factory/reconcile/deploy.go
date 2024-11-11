package reconcile

import (
	"context"
	"fmt"
	"time"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Deployment performs an update or create operator for deployment and waits until it's replicas is ready
func Deployment(ctx context.Context, rclient client.Client, newDeploy, prevDeploy *appsv1.Deployment, hasHPA bool) error {

	var isPrevEqual bool
	if prevDeploy != nil {
		isPrevEqual = equality.Semantic.DeepDerivative(prevDeploy.Spec, newDeploy.Spec)
	}
	rclient.Scheme().Default(newDeploy)

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var currentDeploy appsv1.Deployment
		err := rclient.Get(ctx, types.NamespacedName{Name: newDeploy.Name, Namespace: newDeploy.Namespace}, &currentDeploy)
		if err != nil {
			if errors.IsNotFound(err) {
				if err := rclient.Create(ctx, newDeploy); err != nil {
					return fmt.Errorf("cannot create new deployment for app: %s, err: %w", newDeploy.Name, err)
				}
				return waitDeploymentReady(ctx, rclient, newDeploy, appWaitReadyDeadline)
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
		newDeploy.Status = currentDeploy.Status
		newDeploy.Annotations = labels.Merge(currentDeploy.Annotations, newDeploy.Annotations)
		vmv1beta1.AddFinalizer(newDeploy, &currentDeploy)

		isEqual := equality.Semantic.DeepDerivative(newDeploy.Spec, currentDeploy.Spec)
		if isEqual &&
			isPrevEqual &&
			equality.Semantic.DeepEqual(newDeploy.Labels, currentDeploy.Labels) &&
			equality.Semantic.DeepEqual(newDeploy.Annotations, currentDeploy.Annotations) {
			return waitDeploymentReady(ctx, rclient, newDeploy, appWaitReadyDeadline)
		}
		logger.WithContext(ctx).Info("updating deployment configuration",
			"is_prev_equal", isPrevEqual, "is_current_equal", isEqual,
			"is_prev_nil", prevDeploy == nil)

		if err := rclient.Update(ctx, newDeploy); err != nil {
			return fmt.Errorf("cannot update deployment for app: %s, err: %w", newDeploy.Name, err)
		}

		return waitDeploymentReady(ctx, rclient, newDeploy, appWaitReadyDeadline)
	})
}

// waitDeploymentReady waits until deployment's replicaSet rollouts and all new pods is ready
func waitDeploymentReady(ctx context.Context, rclient client.Client, dep *appsv1.Deployment, deadline time.Duration) error {
	err := wait.PollUntilContextTimeout(ctx, time.Second, deadline, false, func(ctx context.Context) (done bool, err error) {
		var actualDeploy appsv1.Deployment
		if err := rclient.Get(ctx, types.NamespacedName{Namespace: dep.Namespace, Name: dep.Name}, &actualDeploy); err != nil {
			return false, fmt.Errorf("cannot fetch actual deployment state: %w", err)
		}
		cond := getDeploymentCondition(actualDeploy.Status, appsv1.DeploymentProgressing)
		if cond != nil && cond.Reason == "ProgressDeadlineExceeded" {
			return false, fmt.Errorf("deployment %s/%s has exceeded its progress deadline", dep.Namespace, dep.Name)
		}
		if actualDeploy.Spec.Replicas != nil && actualDeploy.Status.UpdatedReplicas < *actualDeploy.Spec.Replicas {
			// Waiting for deployment rollout to finish: part of new replicas have been updated...
			return false, nil
		}
		if actualDeploy.Status.Replicas > actualDeploy.Status.UpdatedReplicas {
			// Waiting for deployment rollout to finish: part of old replicas are pending termination...
			return false, nil
		}
		if actualDeploy.Status.AvailableReplicas < actualDeploy.Status.UpdatedReplicas {
			// Waiting for deployment rollout to finish: part of updated replicas are available
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return reportFirstNotReadyPodOnError(ctx, rclient, fmt.Errorf("cannot wait for deployment to become ready: %w", err), dep.Namespace, labels.SelectorFromSet(dep.Spec.Selector.MatchLabels), dep.Spec.MinReadySeconds)
	}
	return nil
}

func getDeploymentCondition(status appsv1.DeploymentStatus, condType appsv1.DeploymentConditionType) *appsv1.DeploymentCondition {
	for i := range status.Conditions {
		c := status.Conditions[i]
		if c.Type == condType {
			return &c
		}
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
