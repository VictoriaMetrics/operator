package reconcile

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
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
func mergeMaps(currentA, newA, prevA map[string]string) map[string]string {
	dst := make(map[string]string)
	var deleted map[string]struct{}

	for k := range prevA {
		if _, ok := newA[k]; !ok {
			if deleted == nil {
				deleted = make(map[string]struct{})
			}
			deleted[k] = struct{}{}
		}
	}

	for k, v := range currentA {
		if _, ok := deleted[k]; ok {
			continue
		}
		dst[k] = v
	}
	for k, v := range newA {
		dst[k] = v
	}
	return dst
}

// areMapsEqual properly track changes in maps
// it doesn't take into account 3rd party values
func areMapsEqual(existingMap, newMap, prevMap map[string]string) bool {
	for k, v := range newMap {
		cv, ok := existingMap[k]
		if !ok {
			return false
		}
		if v != cv {
			return false
		}
	}
	for k := range prevMap {
		_, nok := newMap[k]
		_, cok := existingMap[k]
		// case for annotations delete
		if nok != cok {
			return false
		}
	}
	return true
}

// isObjectMetaEqual properly track changes at object annotations
// it preserves 3rd party annotations
func isObjectMetaEqual(currObj, newObj, prevObj client.Object) bool {
	var prevLabels, prevAnnotations map[string]string
	if prevObj != nil && !reflect.ValueOf(prevObj).IsNil() {
		prevLabels = prevObj.GetLabels()
		prevAnnotations = prevObj.GetAnnotations()
	}
	annotationsEqual := areMapsEqual(currObj.GetAnnotations(), newObj.GetAnnotations(), prevAnnotations)
	labelsEqual := areMapsEqual(currObj.GetLabels(), newObj.GetLabels(), prevLabels)

	return annotationsEqual && labelsEqual
}

func mergeObjectMetadataIntoNew(currObj, newObj, prevObj client.Object) {
	// empty ResourceVersion for some resources produces the following error
	// is invalid: metadata.resourceVersion: Invalid value: 0x0: must be specified for an update
	// so keep it from current resource
	//
	newObj.SetResourceVersion(currObj.GetResourceVersion())
	// Keep common metadata for consistency sake
	newObj.SetGeneration(currObj.GetGeneration())
	newObj.SetCreationTimestamp(currObj.GetCreationTimestamp())
	newObj.SetUID(currObj.GetUID())
	newObj.SetSelfLink(currObj.GetSelfLink())

	var prevLabels, prevAnnotations map[string]string
	if prevObj != nil && !reflect.ValueOf(prevObj).IsNil() {
		prevLabels = prevObj.GetLabels()
		prevAnnotations = prevObj.GetAnnotations()
	}
	annotations := mergeMaps(currObj.GetAnnotations(), newObj.GetAnnotations(), prevAnnotations)
	labels := mergeMaps(currObj.GetLabels(), newObj.GetLabels(), prevLabels)

	newObj.SetAnnotations(annotations)
	newObj.SetLabels(labels)
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

func addFinalizerIfAbsent(obj client.Object) {
	_ = controllerutil.AddFinalizer(obj, vmv1beta1.FinalizerName)
}

// needsGarbageCollection checks if resource must be freed from finalizer and garbage collected by kubernetes
func needsGarbageCollection(ctx context.Context, rclient client.Client, obj client.Object) error {
	if obj.GetDeletionTimestamp().IsZero() {
		// fast path
		return nil
	}
	if err := finalize.RemoveFinalizer(ctx, rclient, obj); err != nil {
		return fmt.Errorf("cannot remove finalizer from %s=%s/%s: %w", obj.GetObjectKind().GroupVersionKind().Kind, obj.GetNamespace(), obj.GetName(), err)
	}
	return newErrRecreate(ctx, obj)
}
