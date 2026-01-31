package reconcile

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

// Deployment performs an update or create operator for deployment and waits until it's replicas is ready
func Deployment(ctx context.Context, rclient client.Client, newObj, prevObj *appsv1.Deployment, hasHPA bool) error {

	var isPrevEqual bool
	var prevSpecDiff string
	var prevMeta *metav1.ObjectMeta
	if prevObj != nil {
		prevMeta = &prevObj.ObjectMeta
		isPrevEqual = equality.Semantic.DeepDerivative(prevObj.Spec, newObj.Spec)
		if !isPrevEqual {
			prevSpecDiff = diffDeepDerivative(prevObj.Spec, newObj.Spec)
		}
	}
	rclient.Scheme().Default(newObj)

	nsn := types.NamespacedName{Name: newObj.Name, Namespace: newObj.Namespace}
	return retryOnConflict(func() error {
		var existingObj appsv1.Deployment
		if err := rclient.Get(ctx, nsn, &existingObj); err != nil {
			if k8serrors.IsNotFound(err) {
				logger.WithContext(ctx).Info(fmt.Sprintf("creating new Deployment=%s", nsn))
				if err := rclient.Create(ctx, newObj); err != nil {
					return fmt.Errorf("cannot create new Deployment=%s: %w", nsn, err)
				}
				return waitDeploymentReady(ctx, rclient, newObj, appWaitReadyDeadline)
			}
			return fmt.Errorf("cannot get Deployment=%s: %w", nsn, err)
		}
		if err := freeIfNeeded(ctx, rclient, &existingObj); err != nil {
			return err
		}
		if hasHPA {
			newObj.Spec.Replicas = existingObj.Spec.Replicas
		}
		newObj.Status = existingObj.Status
		var prevTemplateAnnotations map[string]string
		if prevObj != nil {
			prevTemplateAnnotations = prevObj.Spec.Template.Annotations
		}
		isEqual := equality.Semantic.DeepDerivative(newObj.Spec, existingObj.Spec)
		if isEqual &&
			isPrevEqual &&
			equality.Semantic.DeepEqual(newObj.Labels, existingObj.Labels) &&
			isObjectMetaEqual(&existingObj, newObj, prevMeta) {
			return waitDeploymentReady(ctx, rclient, newObj, appWaitReadyDeadline)
		}

		vmv1beta1.AddFinalizer(newObj, &existingObj)
		newObj.Spec.Template.Annotations = mergeAnnotations(existingObj.Spec.Template.Annotations, newObj.Spec.Template.Annotations, prevTemplateAnnotations)
		mergeObjectMetadataIntoNew(&existingObj, newObj, prevMeta)

		logMsg := fmt.Sprintf("updating Deployment=%s configuration"+
			"is_prev_equal=%v,is_current_equal=%v,is_prev_nil=%v",
			nsn, isPrevEqual, isEqual, prevObj == nil)

		if len(prevSpecDiff) > 0 {
			logMsg += fmt.Sprintf(", prev_spec_diff=%s", prevSpecDiff)
		}
		if !isEqual {
			logMsg += fmt.Sprintf(", curr_spec_diff=%s", diffDeepDerivative(newObj.Spec, existingObj.Spec))
		}

		logger.WithContext(ctx).Info(logMsg)

		if err := rclient.Update(ctx, newObj); err != nil {
			return fmt.Errorf("cannot update Deployment=%s: %w", nsn, err)
		}

		return waitDeploymentReady(ctx, rclient, newObj, appWaitReadyDeadline)
	})
}

// waitDeploymentReady waits until deployment's replicaSet rollouts and all new pods is ready
func waitDeploymentReady(ctx context.Context, rclient client.Client, dep *appsv1.Deployment, deadline time.Duration) error {
	var isErrDeadline bool
	err := wait.PollUntilContextTimeout(ctx, time.Second, deadline, true, func(ctx context.Context) (done bool, err error) {
		var actualDeploy appsv1.Deployment
		if err := rclient.Get(ctx, types.NamespacedName{Namespace: dep.Namespace, Name: dep.Name}, &actualDeploy); err != nil {
			return false, fmt.Errorf("cannot fetch actual deployment state: %w", err)
		}
		// Based on recommendations from the kubernetes documentation
		// (https://kubernetes.io/docs/concepts/workloads/controllers/deployment/#complete-deployment)
		// this function uses the deployment readiness detection algorithm from `kubectl rollout status` command
		// (https://github.com/kubernetes/kubectl/blob/6e4fe32a45fdcbf61e5c30ebdc511d75e7242432/pkg/polymorphichelpers/rollout_status.go#L76)
		if actualDeploy.Generation > actualDeploy.Status.ObservedGeneration ||
			// special case to prevent possible race condition between updated object and local cache
			// See this issue https://github.com/VictoriaMetrics/operator/issues/1579
			dep.Generation > actualDeploy.Generation {
			// Waiting for deployment spec update to be observed by controller...
			return false, nil
		}
		cond := getDeploymentCondition(actualDeploy.Status, appsv1.DeploymentProgressing)
		if cond != nil && cond.Reason == "ProgressDeadlineExceeded" {
			isErrDeadline = true
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
		if isErrDeadline {
			return err
		}
		return podErr
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
	return &errWaitReady{
		origin: fmt.Errorf("cannot find any pod for selector=%q, check kubernetes events: %w", selector.String(), origin),
	}
}
