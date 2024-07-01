package controller

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"reflect"
	"sync"
	"time"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

// BindFlags binds package flags to the given flagSet
func BindFlags(f *flag.FlagSet) {
	cacheSyncTimeout = f.Duration("controller.cacheSyncTimeout", *cacheSyncTimeout, "controls timeout for caches to be synced.")
	maxConcurrency = f.Int("controller.maxConcurrentReconciles", *maxConcurrency, "Configures number of concurrent reconciles. It should improve performance for clusters with many objects.")
}

var (
	cacheSyncTimeout = ptr.To(3 * time.Minute)
	maxConcurrency   = ptr.To(1)
)

var (
	optionsInit    sync.Once
	defaultOptions *controller.Options
)

var (
	parseObjectErrorsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "operator_controller_object_parsing_errors_total",
		Help: "Counts number of objects, that was failed to parse from json",
	}, []string{"controller", "namespaced_name"})
	getObjectsErrorsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "operator_controller_object_get_errors_total",
		Help: "Counts number of errors for client.Get method at reconciliation loop",
	}, []string{"controller", "namespaced_name"})
	conflictErrorsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "operator_controller_reconcile_conflict_errors_total",
		Help: "Counts number of errors with race conditions, when object was modified by external program at reconciliation",
	}, []string{"controller", "namespaced_name"})
	contextCancelErrorsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "operator_controller_reconcile_errors_total",
		Help: "Counts number contex.Canceled errors",
	})
)

// InitMetrics adds metrics to the Registry
func init() {
	metrics.Registry.MustRegister(parseObjectErrorsTotal, getObjectsErrorsTotal, conflictErrorsTotal, contextCancelErrorsTotal)
}

func getDefaultOptions() controller.Options {
	optionsInit.Do(func() {
		defaultOptions = &controller.Options{
			RateLimiter:             workqueue.NewItemExponentialFailureRateLimiter(2*time.Second, 2*time.Minute),
			CacheSyncTimeout:        *cacheSyncTimeout,
			MaxConcurrentReconciles: *maxConcurrency,
		}
	})
	return *defaultOptions
}

// parsingError usually occurs in case of x-preserve-unknow-fields option enable to CRD
// in this case k8s api server cannot perform proper validation and it may result in bad user input for some fields
type parsingError struct {
	origin     string
	controller string
}

func (pe *parsingError) Error() string {
	return fmt.Sprintf("parsing object error for object controller=%q: %q",
		pe.controller, pe.origin)
}

// getError could usually occur at following cases:
// - not enough k8s permissions
// - object was deleted and due to race condition queue by operator cache
type getError struct {
	origin        error
	controller    string
	requestObject ctrl.Request
}

// Unwrap implemnets errors.Unwrap interface
func (ge *getError) Unwrap() error {
	return ge.origin
}

func (ge *getError) Error() string {
	return fmt.Sprintf("get_object error for controller=%q object_name=%q at namespace=%q, origin=%q", ge.controller, ge.requestObject.Name, ge.requestObject.Namespace, ge.origin)
}

func handleReconcileErr(ctx context.Context, rclient client.Client, object objectWithStatusTrack, originResult ctrl.Result, err error) (ctrl.Result, error) {
	if err == nil {
		return originResult, nil
	}
	var ge *getError
	var pe *parsingError
	switch {
	case errors.Is(err, context.Canceled):
		contextCancelErrorsTotal.Inc()
		return originResult, nil
	case errors.As(err, &pe):
		if err := object.SetUpdateStatusTo(ctx, rclient, vmv1beta1.UpdateStatusFailed, err); err != nil {
			logger.WithContext(ctx).Error(err, "failed to status with parsing error")
		}
		parseObjectErrorsTotal.WithLabelValues(pe.controller, fmt.Sprintf("%s/%s", object.GetNamespace(), object.GetName())).Inc()
	case errors.As(err, &ge):
		deregisterObjectByCollector(ge.requestObject.Name, ge.requestObject.Namespace, ge.controller)
		getObjectsErrorsTotal.WithLabelValues(ge.controller, ge.requestObject.String()).Inc()
		if apierrors.IsNotFound(err) {
			err = nil
			return originResult, nil
		}
	case apierrors.IsConflict(err):
		controller := "unknown"
		namespacedName := "unknown"
		if object != nil && !reflect.ValueOf(object).IsNil() && object.GetNamespace() != "" {
			controller = object.GetObjectKind().GroupVersionKind().GroupKind().Kind
			namespacedName = fmt.Sprintf("%s/%s", object.GetNamespace(), object.GetName())
		}
		conflictErrorsTotal.WithLabelValues(controller, namespacedName).Inc()
		return ctrl.Result{RequeueAfter: time.Second * 5}, nil
	}
	if object != nil && !reflect.ValueOf(object).IsNil() && object.GetNamespace() != "" {
		errEvent := &corev1.Event{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "victoria-metrics-operator-" + uuid.New().String(),
				Namespace: object.GetNamespace(),
			},
			Type:    corev1.EventTypeWarning,
			Reason:  "ReconcilationError",
			Message: err.Error(),
			Source: corev1.EventSource{
				Component: "victoria-metrics-operator",
			},
			LastTimestamp: metav1.NewTime(time.Now()),
			InvolvedObject: corev1.ObjectReference{
				Kind:            object.GetObjectKind().GroupVersionKind().Kind,
				Namespace:       object.GetNamespace(),
				Name:            object.GetName(),
				UID:             object.GetUID(),
				ResourceVersion: object.GetResourceVersion(),
			},
		}
		if err := rclient.Create(ctx, errEvent); err != nil {
			logger.WithContext(ctx).Error(err, "failed to create error event at kubernetes API during reconciliation error")
		}
	}

	return originResult, err
}

func isNamespaceSelectorMatches(ctx context.Context, rclient client.Client, sourceCRD, targetCRD client.Object, selector *metav1.LabelSelector) (bool, error) {
	switch {
	case selector == nil:
		if sourceCRD.GetNamespace() == targetCRD.GetNamespace() {
			return true, nil
		}
		return false, nil
	case len(selector.MatchLabels) == 0 && len(selector.MatchExpressions) == 0:
		return true, nil
	case len(config.MustGetWatchNamespaces()) > 0:
		// selector labels for namespace ignores by default for multi-namespace mode
		return true, nil
	}

	ns := &corev1.NamespaceList{}
	nsSelector, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return false, fmt.Errorf("cannot convert namespace selector: %w", err)
	}
	if err := rclient.List(ctx, ns, &client.ListOptions{LabelSelector: nsSelector}); err != nil {
		return false, err
	}

	for _, n := range ns.Items {
		if n.Name == targetCRD.GetNamespace() {
			return true, nil
		}
	}
	return false, nil
}

func isSelectorsMatchesTargetCRD(ctx context.Context, rclient client.Client, sourceCRD, targetCRD client.Object, selector, namespaceSelector *metav1.LabelSelector) (bool, error) {
	// check namespace selector
	if isNsMatch, err := isNamespaceSelectorMatches(ctx, rclient, sourceCRD, targetCRD, namespaceSelector); !isNsMatch || err != nil {
		return isNsMatch, err
	}
	// in case of empty namespace object must be synchronized in any way,
	// coz we dont know source labels.
	// probably object already deleted.
	if sourceCRD.GetNamespace() == "" {
		return true, nil
	}

	// filter selector label.
	if selector == nil {
		return true, nil
	}

	labelSelector, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return false, fmt.Errorf("cannot parse ruleSelector selector as labelSelector: %w", err)
	}
	set := labels.Set(sourceCRD.GetLabels())
	// selector not match
	if !labelSelector.Matches(set) {
		return false, nil
	}
	return true, nil
}

type objectWithStatusTrack interface {
	client.Object
	HasSpecChanges() (bool, error)
	LastAppliedSpecAsPatch() (client.Patch, error)
	SetUpdateStatusTo(ctx context.Context, r client.Client, status vmv1beta1.UpdateStatus, maybeReason error) error
	Paused() bool
}

func createGenericEventForObject(ctx context.Context, c client.Client, object client.Object, message string) error {
	ev := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "victoria-metrics-operator-" + uuid.New().String(),
			Namespace: object.GetNamespace(),
		},
		Type:    corev1.EventTypeNormal,
		Reason:  "ReconcileEvent",
		Message: message,
		Source: corev1.EventSource{
			Component: "victoria-metrics-operator",
		},
		LastTimestamp: metav1.NewTime(time.Now()),
		InvolvedObject: corev1.ObjectReference{
			Kind:            object.GetObjectKind().GroupVersionKind().Kind,
			Namespace:       object.GetNamespace(),
			Name:            object.GetName(),
			UID:             object.GetUID(),
			ResourceVersion: object.GetResourceVersion(),
		},
	}
	if err := c.Create(ctx, ev); err != nil {
		return fmt.Errorf("cannot create generic event at k8s api for object: %q: %w", object.GetObjectKind().GroupVersionKind().GroupKind(), err)
	}
	return nil
}

func reconcileAndTrackStatus(ctx context.Context, c client.Client, object objectWithStatusTrack, cb func() (ctrl.Result, error)) (result ctrl.Result, resultErr error) {
	if object.Paused() {
		if err := object.SetUpdateStatusTo(ctx, c, vmv1beta1.UpdateStatusPaused, nil); err != nil {
			resultErr = fmt.Errorf("failed to update object status: %w", err)
			return
		}
		return
	}
	specChanged, err := object.HasSpecChanges()
	if err != nil {
		resultErr = fmt.Errorf("cannot parse exist spec changes")
		return
	}
	if specChanged {
		if err := object.SetUpdateStatusTo(ctx, c, vmv1beta1.UpdateStatusExpanding, nil); err != nil {
			resultErr = fmt.Errorf("failed to update object status: %w", err)
			return
		}
		if err := createGenericEventForObject(ctx, c, object, "starting object update"); err != nil {
			logger.WithContext(ctx).Error(err, " cannot create k8s api event")
		}
	}

	result, err = cb()
	if err != nil {
		if updateErr := object.SetUpdateStatusTo(ctx, c, vmv1beta1.UpdateStatusFailed, err); updateErr != nil {
			resultErr = fmt.Errorf("failed to update object status: %q, origin err: %w", updateErr, err)
			return
		}

		return result, err
	}

	if err := object.SetUpdateStatusTo(ctx, c, vmv1beta1.UpdateStatusOperational, nil); err != nil {
		resultErr = fmt.Errorf("failed to update object status: %w", err)
		return
	}
	if specChanged {
		specPatch, err := object.LastAppliedSpecAsPatch()
		if err != nil {
			resultErr = fmt.Errorf("cannot parse last applied spec for cluster: %w", err)
			return
		}
		// use patch instead of update, only 1 field must be changed.
		if err := c.Patch(ctx, object, specPatch); err != nil {
			resultErr = fmt.Errorf("cannot update cluster with last applied spec: %w", err)
			return
		}
		if err := createGenericEventForObject(ctx, c, object, "reconcile of object finished successfully"); err != nil {
			logger.WithContext(ctx).Error(err, " cannot create k8s api event")
		}
	}

	return result, nil
}
