package reconcile

import (
	"context"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

// Deployment performs an update or create operator for deployment and waits until it's replicas is ready
func Deployment(ctx context.Context, rclient client.Client, newObj, prevObj *appsv1.Deployment, hasHPA bool, owner *metav1.OwnerReference) error {
	var prevMeta *metav1.ObjectMeta
	var prevTemplateAnnotations map[string]string
	if prevObj != nil {
		prevMeta = &prevObj.ObjectMeta
		prevTemplateAnnotations = prevObj.Spec.Template.Annotations
	}
	rclient.Scheme().Default(newObj)
	nsn := types.NamespacedName{Name: newObj.Name, Namespace: newObj.Namespace}
	err := retryOnConflict(func() error {
		var existingObj appsv1.Deployment
		if err := rclient.Get(ctx, nsn, &existingObj); err != nil {
			if k8serrors.IsNotFound(err) {
				logger.WithContext(ctx).Info(fmt.Sprintf("creating new Deployment=%s", nsn))
				if err := rclient.Create(ctx, newObj); err != nil {
					return fmt.Errorf("cannot create new Deployment=%s: %w", nsn, err)
				}
				return nil
			}
			return fmt.Errorf("cannot get Deployment=%s: %w", nsn, err)
		}
		if err := collectGarbage(ctx, rclient, &existingObj); err != nil {
			return err
		}
		spec := &newObj.Spec
		if hasHPA {
			spec.Replicas = existingObj.Spec.Replicas
		}
		metaChanged, err := mergeMeta(&existingObj, newObj, prevMeta, owner)
		if err != nil {
			return err
		}
		logMessageMetadata := []string{fmt.Sprintf("name=%s, is_prev_nil=%t", nsn, prevObj == nil)}
		spec.Template.Annotations = mergeMaps(existingObj.Spec.Template.Annotations, newObj.Spec.Template.Annotations, prevTemplateAnnotations)
		specDiff := diffDeepDerivative(newObj.Spec, existingObj.Spec)
		needsUpdate := metaChanged || len(specDiff) > 0
		logMessageMetadata = append(logMessageMetadata, fmt.Sprintf("spec_diff=%s", specDiff))
		if !needsUpdate {
			return nil
		}
		existingObj.Spec = newObj.Spec
		logger.WithContext(ctx).Info(fmt.Sprintf("updating Deployment %s", strings.Join(logMessageMetadata, ", ")))
		if err := rclient.Update(ctx, &existingObj); err != nil {
			return fmt.Errorf("cannot update Deployment=%s: %w", nsn, err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	return waitForDeploymentReady(ctx, rclient, newObj, appWaitReadyDeadline)
}

// waitForDeploymentReady waits until deployment's replicaSet rollouts and all new pods is ready
func waitForDeploymentReady(ctx context.Context, rclient client.Client, newObj *appsv1.Deployment, deadline time.Duration) error {
	if newObj.Spec.Replicas == nil {
		return nil
	}
	var isErrDeadline bool
	nsn := types.NamespacedName{Namespace: newObj.Namespace, Name: newObj.Name}
	err := wait.PollUntilContextTimeout(ctx, time.Second, deadline, true, func(ctx context.Context) (done bool, err error) {
		var existingObj appsv1.Deployment
		if err := rclient.Get(ctx, nsn, &existingObj); err != nil {
			if k8serrors.IsNotFound(err) {
				return false, nil
			}
			return false, fmt.Errorf("cannot fetch actual Deployment=%s state: %w", nsn, err)
		}
		// Based on recommendations from the kubernetes documentation
		// (https://kubernetes.io/docs/concepts/workloads/controllers/deployment/#complete-deployment)
		// this function uses the deployment readiness detection algorithm from `kubectl rollout status` command
		// (https://github.com/kubernetes/kubectl/blob/6e4fe32a45fdcbf61e5c30ebdc511d75e7242432/pkg/polymorphichelpers/rollout_status.go#L76)
		if existingObj.Generation > existingObj.Status.ObservedGeneration ||
			// special case to prevent possible race condition between updated object and local cache
			// See this issue https://github.com/VictoriaMetrics/operator/issues/1579
			newObj.Generation > existingObj.Generation {
			// Waiting for deployment spec update to be observed by controller...
			return false, nil
		}
		cond := getDeploymentCondition(existingObj.Status, appsv1.DeploymentProgressing)
		if cond != nil && cond.Reason == "ProgressDeadlineExceeded" {
			isErrDeadline = true
			return true, fmt.Errorf("progress deadline exceeded for Deployment=%s", nsn)
		}
		if *newObj.Spec.Replicas != existingObj.Status.ReadyReplicas || *newObj.Spec.Replicas != existingObj.Status.UpdatedReplicas {
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		if isErrDeadline {
			return err
		}
		return reportFirstNotReadyPodOnError(ctx, rclient, fmt.Errorf("cannot wait for Deployment=%s to become ready: %w", nsn, err), newObj.Namespace, labels.SelectorFromSet(newObj.Spec.Selector.MatchLabels), newObj.Spec.MinReadySeconds)
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
	for _, pod := range podList.Items {
		if !pod.DeletionTimestamp.IsZero() || PodIsReady(&pod, minReadySeconds) {
			continue
		}
		return podStatusesToError(origin, &pod)
	}
	return fmt.Errorf("cannot find any pod for selector=%q, check kubernetes events: %w", selector.String(), origin)
}
