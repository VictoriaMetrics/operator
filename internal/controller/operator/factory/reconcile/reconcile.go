package reconcile

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

var (
	podWaitReadyIntervalCheck = 50 * time.Millisecond
	appWaitReadyDeadline      = 5 * time.Second
	podWaitReadyTimeout       = 5 * time.Second
	vmStatusInterval          = 5 * time.Second
)

// InitFromConfig sets package configuration from config
func InitDeadlines(intervalCheck, appWaitDeadline, podReadyDeadline time.Duration) {
	podWaitReadyIntervalCheck = intervalCheck
	appWaitReadyDeadline = appWaitDeadline
	podWaitReadyTimeout = podReadyDeadline
}

// mergeMaps performs 3-way merge for labels and annotations
// it deletes only labels and annotations managed by operator CRDs
// 3-rd party kubernetes annotations and labels must be preserved
func mergeMaps(existingMap, newMap, prevMap map[string]string) map[string]string {
	var dst map[string]string
	var deleted map[string]struct{}

	for k := range prevMap {
		if _, ok := newMap[k]; !ok {
			if deleted == nil {
				deleted = make(map[string]struct{})
			}
			deleted[k] = struct{}{}
		}
	}

	for k, v := range existingMap {
		if _, ok := deleted[k]; ok {
			continue
		}
		if dst == nil {
			dst = make(map[string]string)
		}
		dst[k] = v
	}
	for k, v := range newMap {
		if dst == nil {
			dst = make(map[string]string)
		}
		dst[k] = v
	}
	return dst
}

func mergeMeta(existingObj, newObj client.Object, prevMeta *metav1.ObjectMeta, owner *metav1.OwnerReference) (bool, error) {
	refChanged, err := addOwnerReferenceIfAbsent(existingObj, owner)
	if err != nil {
		return false, err
	}
	finChanged := controllerutil.AddFinalizer(existingObj, vmv1beta1.FinalizerName)
	existingLabels := existingObj.GetLabels()
	existingAnnotations := existingObj.GetAnnotations()
	var prevLabels, prevAnnotations map[string]string
	if prevMeta != nil {
		prevLabels = prevMeta.Labels
		prevAnnotations = prevMeta.Annotations
	}
	newLabels := mergeMaps(existingLabels, newObj.GetLabels(), prevLabels)
	newAnnotations := mergeMaps(existingAnnotations, newObj.GetAnnotations(), prevAnnotations)
	changed := refChanged || finChanged || !equality.Semantic.DeepEqual(existingLabels, newLabels) ||
		!equality.Semantic.DeepEqual(existingAnnotations, newAnnotations)
	existingObj.SetLabels(newLabels)
	existingObj.SetAnnotations(newAnnotations)
	return changed, nil
}

func isRecreate(err error) bool {
	var e *errRecreate
	return errors.As(err, &e)
}

type errRecreate struct {
	msg string
}

func newErrRecreate(ctx context.Context, r client.Object) *errRecreate {
	finalizers := strings.Join(r.GetFinalizers(), ",")
	msg := fmt.Sprintf("waiting for %s=%s/%s (finalizers=[%s]) to be removed", r.GetObjectKind().GroupVersionKind().Kind, r.GetNamespace(), r.GetName(), finalizers)
	logger.WithContext(ctx).Info(msg)
	return &errRecreate{
		msg: msg,
	}
}

// Error implements errors.Error interface
func (e *errRecreate) Error() string {
	return e.msg
}

// IsRetryable determines one of errors:
// * error which indicates that timeout for app transition into Ready state reached and should be continued at the next reconcile loop
// * k8s conflict error
// * reconciled resource is being deleted
func IsRetryable(err error) bool {
	return isConflict(err) || wait.Interrupted(err)
}

func isConflict(err error) bool {
	return k8serrors.IsConflict(err) || isRecreate(err)
}

func retryOnConflict(fn func() error) error {
	return retry.OnError(retry.DefaultRetry, isConflict, fn)
}

// waitForStatus waits till obj reaches defined status
func waitForStatus[T client.Object, ST StatusWithMetadata[STC], STC any](
	ctx context.Context,
	rclient client.Client,
	obj ObjectWithDeepCopyAndStatus[T, ST, STC],
	interval time.Duration,
	status vmv1beta1.UpdateStatus,
) error {
	lastStatus := obj.GetStatus().GetStatusMetadata()
	nsn := types.NamespacedName{Name: obj.GetName(), Namespace: obj.GetNamespace()}
	err := wait.PollUntilContextCancel(ctx, interval, false, func(ctx context.Context) (done bool, err error) {
		if err = rclient.Get(ctx, nsn, obj); err != nil {
			if k8serrors.IsNotFound(err) {
				return false, nil
			}
			err = fmt.Errorf("unexpected error during attempt to get %T=%s: %w", obj, nsn, err)
			return
		}
		lastStatus = obj.GetStatus().GetStatusMetadata()
		return lastStatus != nil && obj.GetGeneration() == lastStatus.ObservedGeneration && lastStatus.UpdateStatus == status, nil
	})
	if err != nil {
		updateStatus := "unknown"
		if lastStatus != nil {
			updateStatus = string(lastStatus.UpdateStatus)
		}
		return fmt.Errorf("failed to wait for %T %s to be ready: %w, current status: %s", obj, nsn, err, updateStatus)
	}
	return nil
}

func addOwnerReferenceIfAbsent(obj client.Object, owner *metav1.OwnerReference) (bool, error) {
	if owner == nil {
		return false, nil
	}
	owners := obj.GetOwnerReferences()
	for _, o := range owners {
		if o.APIVersion == owner.APIVersion && o.Kind == owner.Kind {
			if o.Name == owner.Name {
				return false, nil
			} else {
				msg := fmt.Sprintf("object %T=%s/%s has another owner reference of same kind", obj, obj.GetNamespace(), obj.GetName())
				return false, fmt.Errorf("%s: %s/%s=%s", msg, o.APIVersion, o.Kind, o.Name)
			}
		}
	}
	owners = append(owners, *owner)
	obj.SetOwnerReferences(owners)
	return true, nil
}

// collectGarbage checks if resource must be freed from finalizer and prepares it for garbage collection by kubernetes
func collectGarbage(ctx context.Context, rclient client.Client, obj client.Object) error {
	if obj.GetDeletionTimestamp().IsZero() {
		// fast path
		return nil
	}
	if err := finalize.RemoveFinalizer(ctx, rclient, obj); err != nil {
		return fmt.Errorf("cannot remove finalizer from %s=%s/%s: %w", obj.GetObjectKind().GroupVersionKind().Kind, obj.GetNamespace(), obj.GetName(), err)
	}
	return newErrRecreate(ctx, obj)
}
