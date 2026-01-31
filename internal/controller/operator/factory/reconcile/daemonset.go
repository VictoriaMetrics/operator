package reconcile

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
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

// DaemonSet performs an update or create operator for daemonset and waits until it finishes update rollout
func DaemonSet(ctx context.Context, rclient client.Client, newObj, prevObj *appsv1.DaemonSet) error {
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
		var existingObj appsv1.DaemonSet
		if err := rclient.Get(ctx, nsn, &existingObj); err != nil {
			if k8serrors.IsNotFound(err) {
				logger.WithContext(ctx).Info(fmt.Sprintf("creating new DaemonSet=%s", nsn))
				if err := rclient.Create(ctx, newObj); err != nil {
					return fmt.Errorf("cannot create new DaemonSet=%s: %w", nsn, err)
				}
				return waitDaemonSetReady(ctx, rclient, newObj, appWaitReadyDeadline)
			}
			return fmt.Errorf("cannot get DaemonSet=%s: %w", nsn, err)
		}
		if err := freeIfNeeded(ctx, rclient, &existingObj); err != nil {
			return err
		}
		newObj.Status = existingObj.Status

		isEqual := equality.Semantic.DeepDerivative(newObj.Spec, existingObj.Spec)
		if isEqual &&
			isPrevEqual &&
			equality.Semantic.DeepEqual(newObj.Labels, existingObj.Labels) &&
			isObjectMetaEqual(&existingObj, newObj, prevMeta) {
			return waitDaemonSetReady(ctx, rclient, newObj, appWaitReadyDeadline)
		}
		var prevTemplateAnnotations map[string]string
		if prevMeta != nil {
			prevTemplateAnnotations = prevObj.Spec.Template.Annotations
		}

		vmv1beta1.AddFinalizer(newObj, &existingObj)
		newObj.Spec.Template.Annotations = mergeAnnotations(existingObj.Spec.Template.Annotations, newObj.Spec.Template.Annotations, prevTemplateAnnotations)
		mergeObjectMetadataIntoNew(&existingObj, newObj, prevMeta)

		logMsg := fmt.Sprintf("updating DaemonSet=%s configuration"+
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
			return fmt.Errorf("cannot update DaemonSet=%s: %w", nsn, err)
		}

		return waitDaemonSetReady(ctx, rclient, newObj, appWaitReadyDeadline)
	})
}

// waitDeploymentReady waits until deployment's replicaSet rollouts and all new pods is ready
func waitDaemonSetReady(ctx context.Context, rclient client.Client, ds *appsv1.DaemonSet, deadline time.Duration) error {
	var isErrDealine bool
	err := wait.PollUntilContextTimeout(ctx, time.Second, deadline, true, func(ctx context.Context) (done bool, err error) {
		var daemon appsv1.DaemonSet
		if err := rclient.Get(ctx, types.NamespacedName{Namespace: ds.Namespace, Name: ds.Name}, &daemon); err != nil {
			return false, fmt.Errorf("cannot fetch actual daemonset state: %w", err)
		}

		// Based on recommendations from the kubernetes documentation
		// this function uses the deployment readiness detection algorithm from `kubectl rollout status` command
		// https://github.com/kubernetes/kubectl/blob/6e4fe32a45fdcbf61e5c30ebdc511d75e7242432/pkg/polymorphichelpers/rollout_status.go#L95
		if daemon.Generation > daemon.Status.ObservedGeneration {
			// Waiting for deployment spec update to be observed by controller...
			return false, nil
		}
		if daemon.Status.UpdatedNumberScheduled < daemon.Status.DesiredNumberScheduled {
			return false, nil
		}
		if daemon.Status.NumberAvailable < daemon.Status.DesiredNumberScheduled {
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		podErr := reportFirstNotReadyPodOnError(ctx, rclient, fmt.Errorf("cannot wait for DaemonSet to become ready: %w", err), ds.Namespace, labels.SelectorFromSet(ds.Spec.Selector.MatchLabels), ds.Spec.MinReadySeconds)
		if isErrDealine {
			return err
		}
		return podErr
	}
	return nil
}
