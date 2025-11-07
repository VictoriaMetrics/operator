package reconcile

import (
	"errors"
	"fmt"
	"reflect"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

// mergeAnnotations performs 3-way merge for annotations
// it deletes only annotations managed by operator CRDs
// 3-rd party kubernetes annotations must be preserved
func mergeAnnotations(currentA, newA, prevA map[string]string) map[string]string {
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

// isObjectMetaEqual properly track changes at object annotations
// it preserves 3rd party annotations
func isObjectMetaEqual(currObj, newObj, prevObj client.Object) bool {
	isMapsEqual := func(currentA, newA, prevA map[string]string) bool {
		for k, v := range newA {
			cv, ok := currentA[k]
			if !ok {
				return false
			}
			if v != cv {
				return false
			}
		}
		for k := range prevA {
			_, nok := newA[k]
			_, cok := currentA[k]
			// case for annotations delete
			if nok != cok {
				return false
			}
		}
		return true
	}
	var prevLabels, prevAnnotations map[string]string
	if prevObj != nil && !reflect.ValueOf(prevObj).IsNil() {
		prevLabels = prevObj.GetLabels()
		prevAnnotations = prevObj.GetAnnotations()
	}
	annotationsEqual := isMapsEqual(currObj.GetAnnotations(), newObj.GetAnnotations(), prevAnnotations)
	labelsEqual := isMapsEqual(currObj.GetLabels(), newObj.GetLabels(), prevLabels)

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
	annotations := mergeAnnotations(currObj.GetAnnotations(), newObj.GetAnnotations(), prevAnnotations)
	labels := mergeAnnotations(currObj.GetLabels(), newObj.GetLabels(), prevLabels)

	newObj.SetAnnotations(annotations)
	newObj.SetLabels(labels)
}

// IsErrorWaitTimeout determines if the err is an error which indicates that timeout for app
// transition into Ready state reached and should be continued at the next reconcile loop
func IsErrorWaitTimeout(err error) bool {
	var e *errWaitReady
	return errors.As(err, &e)
}

type errWaitReady struct {
	origin error
}

// Error implements errors.Error interface
func (e *errWaitReady) Error() string {
	return fmt.Sprintf(": %q", e.origin)
}

// Unwrap implements error.Unwrap interface
func (e *errWaitReady) Unwrap() error {
	return e.origin
}

func isRecreate(err error) bool {
	var e *errRecreate
	return errors.As(err, &e)
}

type errRecreate struct {
	origin error
}

// Error implements errors.Error interface
func (e *errRecreate) Error() string {
	return fmt.Sprintf(": %q", e.origin)
}

// Unwrap implements error.Unwrap interface
func (e *errRecreate) Unwrap() error {
	return e.origin
}

func isRetryable(err error) bool {
	return k8serrors.IsConflict(err) || isRecreate(err)
}

func retryOnConflict(fn func() error) error {
	return retry.OnError(retry.DefaultRetry, isRetryable, fn)
}
