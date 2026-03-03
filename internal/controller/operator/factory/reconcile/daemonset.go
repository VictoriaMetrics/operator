package reconcile

import (
	"context"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

// DaemonSet performs an update or create operator for daemonset and waits until it finishes update rollout
func DaemonSet(ctx context.Context, rclient client.Client, newObj, prevObj *appsv1.DaemonSet, owner *metav1.OwnerReference) error {
	var prevMeta *metav1.ObjectMeta
	var prevTemplateAnnotations map[string]string
	if prevObj != nil {
		prevMeta = &prevObj.ObjectMeta
		prevTemplateAnnotations = prevObj.Spec.Template.Annotations
	}
	rclient.Scheme().Default(newObj)
	nsn := types.NamespacedName{Name: newObj.Name, Namespace: newObj.Namespace}
	removeFinalizer := true
	err := retryOnConflict(func() error {
		var existingObj appsv1.DaemonSet
		if err := rclient.Get(ctx, nsn, &existingObj); err != nil {
			if k8serrors.IsNotFound(err) {
				logger.WithContext(ctx).Info(fmt.Sprintf("creating new DaemonSet=%s", nsn.String()))
				if err := rclient.Create(ctx, newObj); err != nil {
					return fmt.Errorf("cannot create new DaemonSet=%s: %w", nsn.String(), err)
				}
				return nil
			}
			return fmt.Errorf("cannot get DaemonSet=%s: %w", nsn.String(), err)
		}
		if err := collectGarbage(ctx, rclient, &existingObj, removeFinalizer); err != nil {
			return err
		}
		spec := &newObj.Spec
		metaChanged, err := mergeMeta(&existingObj, newObj, prevMeta, owner, removeFinalizer)
		if err != nil {
			return err
		}

		logMessageMetadata := []string{fmt.Sprintf("name=%s, is_prev_nil=%t", nsn.String(), prevObj == nil)}
		spec.Template.Annotations = mergeMaps(existingObj.Spec.Template.Annotations, newObj.Spec.Template.Annotations, prevTemplateAnnotations)
		specDiff := diffDeepDerivative(newObj.Spec, existingObj.Spec)
		needsUpdate := metaChanged || len(specDiff) > 0
		logMessageMetadata = append(logMessageMetadata, fmt.Sprintf("spec_diff=%s", specDiff))

		if !needsUpdate {
			return nil
		}
		existingObj.Spec = newObj.Spec
		logger.WithContext(ctx).Info(fmt.Sprintf("updating DaemonSet %s", strings.Join(logMessageMetadata, ", ")))
		if err := rclient.Update(ctx, &existingObj); err != nil {
			return fmt.Errorf("cannot update DaemonSet=%s: %w", nsn.String(), err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	return waitDaemonSetReady(ctx, rclient, newObj, appWaitReadyDeadline)
}

// waitDeploymentReady waits until deployment's replicaSet rollouts and all new pods is ready
func waitDaemonSetReady(ctx context.Context, rclient client.Client, ds *appsv1.DaemonSet, deadline time.Duration) error {
	var isErrDealine bool
	nsn := types.NamespacedName{Namespace: ds.Namespace, Name: ds.Name}
	err := wait.PollUntilContextTimeout(ctx, time.Second, deadline, true, func(ctx context.Context) (done bool, err error) {
		var daemon appsv1.DaemonSet
		if err := rclient.Get(ctx, nsn, &daemon); err != nil {
			if k8serrors.IsNotFound(err) {
				return false, nil
			}
			return false, fmt.Errorf("cannot fetch actual DaemonSet=%s: %w", nsn.String(), err)
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
		podErr := reportFirstNotReadyPodOnError(ctx, rclient, fmt.Errorf("cannot wait for DaemonSet=%s to become ready: %w", nsn.String(), err), ds.Namespace, labels.SelectorFromSet(ds.Spec.Selector.MatchLabels), ds.Spec.MinReadySeconds)
		if isErrDealine {
			return err
		}
		return podErr
	}
	return nil
}
