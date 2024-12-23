package reconcile

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
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
				logger.WithContext(ctx).Info(fmt.Sprintf("creating new Deployment %s", newDeploy.Name))
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
		newDeploy.Status = currentDeploy.Status
		var prevAnnotations map[string]string
		if prevDeploy != nil {
			prevAnnotations = prevDeploy.Annotations
		}
		isEqual := equality.Semantic.DeepDerivative(newDeploy.Spec, currentDeploy.Spec)
		if isEqual &&
			isPrevEqual &&
			equality.Semantic.DeepEqual(newDeploy.Labels, currentDeploy.Labels) &&
			isAnnotationsEqual(currentDeploy.Annotations, newDeploy.Annotations, prevAnnotations) {
			return waitDeploymentReady(ctx, rclient, newDeploy, appWaitReadyDeadline)
		}

		vmv1beta1.AddFinalizer(newDeploy, &currentDeploy)
		newDeploy.Annotations = mergeAnnotations(currentDeploy.Annotations, newDeploy.Annotations, prevAnnotations)
		cloneSignificantMetadata(newDeploy, &currentDeploy)

		logger.WithContext(ctx).Info(fmt.Sprintf("updating Deployment %s configuration"+
			"is_prev_equal=%v,is_current_equal=%v,is_prev_nil=%v",
			newDeploy.Name, isPrevEqual, isEqual, prevDeploy == nil))

		if err := rclient.Update(ctx, newDeploy); err != nil {
			return fmt.Errorf("cannot update deployment for app: %s, err: %w", newDeploy.Name, err)
		}

		return waitDeploymentReady(ctx, rclient, newDeploy, appWaitReadyDeadline)
	})
}

// waitDeploymentReady waits until deployment's replicaSet rollouts and all new pods is ready
func waitDeploymentReady(ctx context.Context, rclient client.Client, dep *appsv1.Deployment, deadline time.Duration) error {
	var isErrDealine bool
	err := wait.PollUntilContextTimeout(ctx, time.Second, deadline, false, func(ctx context.Context) (done bool, err error) {
		var actualDeploy appsv1.Deployment
		if err := rclient.Get(ctx, types.NamespacedName{Namespace: dep.Namespace, Name: dep.Name}, &actualDeploy); err != nil {
			return false, fmt.Errorf("cannot fetch actual deployment state: %w", err)
		}
		// Based on recommendations from the kubernetes documentation
		// (https://kubernetes.io/docs/concepts/workloads/controllers/deployment/#complete-deployment)
		// this function uses the deployment readiness detection algorithm from `kubectl rollout status` command
		// (https://github.com/kubernetes/kubectl/blob/6e4fe32a45fdcbf61e5c30ebdc511d75e7242432/pkg/polymorphichelpers/rollout_status.go#L76)
		if actualDeploy.Generation > actualDeploy.Status.ObservedGeneration {
			// Waiting for deployment spec update to be observed by controller...
			return false, nil
		}
		cond := getDeploymentCondition(actualDeploy.Status, appsv1.DeploymentProgressing)
		if cond != nil && cond.Reason == "ProgressDeadlineExceeded" {
			isErrDealine = true
			return true, fmt.Errorf("deployment %s/%s has exceeded its progress deadline", dep.Namespace, dep.Name)
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
		podErr := reportFirstNotReadyPodOnError(ctx, rclient, fmt.Errorf("cannot wait for deployment to become ready: %w", err), dep.Namespace, labels.SelectorFromSet(dep.Spec.Selector.MatchLabels), dep.Spec.MinReadySeconds)
		if isErrDealine {
			return err
		}
		return &errWaitReady{origin: podErr}
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
