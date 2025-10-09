package reconcile

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

// DaemonSet performs an update or create operator for daemonset and waits until it finishes update rollout
func DaemonSet(ctx context.Context, rclient client.Client, newDs, prevDs *appsv1.DaemonSet) error {
	var isPrevEqual bool
	var prevSpecDiff string
	if prevDs != nil {
		isPrevEqual = equality.Semantic.DeepDerivative(prevDs.Spec, newDs.Spec)
		if !isPrevEqual {
			prevSpecDiff = diffDeepDerivative(prevDs.Spec, newDs.Spec)
		}
	}
	rclient.Scheme().Default(newDs)

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var currentDs appsv1.DaemonSet
		err := rclient.Get(ctx, types.NamespacedName{Name: newDs.Name, Namespace: newDs.Namespace}, &currentDs)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				logger.WithContext(ctx).Info(fmt.Sprintf("creating new DaemonSet %s", newDs.Name))
				if err := rclient.Create(ctx, newDs); err != nil {
					return fmt.Errorf("cannot create new DaemonSet for app: %s, err: %w", newDs.Name, err)
				}
				return waitDaemonSetReady(ctx, rclient, newDs, appWaitReadyDeadline)
			}
			return fmt.Errorf("cannot get DaemonSet for app: %s err: %w", newDs.Name, err)
		}
		if err := finalize.FreeIfNeeded(ctx, rclient, &currentDs); err != nil {
			return err
		}
		newDs.Status = currentDs.Status

		isEqual := equality.Semantic.DeepDerivative(newDs.Spec, currentDs.Spec)
		if isEqual &&
			isPrevEqual &&
			equality.Semantic.DeepEqual(newDs.Labels, currentDs.Labels) &&
			isObjectMetaEqual(&currentDs, newDs, prevDs) {
			return waitDaemonSetReady(ctx, rclient, newDs, appWaitReadyDeadline)
		}
		var prevTemplateAnnotations map[string]string
		if prevDs != nil {
			prevTemplateAnnotations = prevDs.Annotations
		}

		vmv1beta1.AddFinalizer(newDs, &currentDs)
		newDs.Spec.Template.Annotations = mergeAnnotations(currentDs.Spec.Template.Annotations, newDs.Spec.Template.Annotations, prevTemplateAnnotations)
		mergeObjectMetadataIntoNew(&currentDs, newDs, prevDs)

		logMsg := fmt.Sprintf("updating DaemonSet %s configuration"+
			"is_prev_equal=%v,is_current_equal=%v,is_prev_nil=%v",
			newDs.Name, isPrevEqual, isEqual, prevDs == nil)

		if len(prevSpecDiff) > 0 {
			logMsg += fmt.Sprintf(", prev_spec_diff=%s", prevSpecDiff)
		}
		if !isEqual {
			logMsg += fmt.Sprintf(", curr_spec_diff=%s", diffDeepDerivative(newDs.Spec, currentDs.Spec))
		}

		logger.WithContext(ctx).Info(logMsg)

		if err := rclient.Update(ctx, newDs); err != nil {
			return fmt.Errorf("cannot update DaemonSet for app: %s, err: %w", newDs.Name, err)
		}

		return waitDaemonSetReady(ctx, rclient, newDs, appWaitReadyDeadline)
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
