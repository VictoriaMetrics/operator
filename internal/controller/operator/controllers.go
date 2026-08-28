package operator

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	k8sreconcile "sigs.k8s.io/controller-runtime/pkg/reconcile"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

// BindFlags binds package flags to the given flagSet
func BindFlags(f *flag.FlagSet) {
	cacheSyncTimeout = f.Duration("controller.cacheSyncTimeout", *cacheSyncTimeout, "controls timeout for caches to be synced.")
	maxConcurrency = f.Int("controller.maxConcurrentReconciles", *maxConcurrency, "Configures number of concurrent reconciles. It should improve performance for clusters with many objects.")
}

var (
	cacheSyncTimeout = ptr.To(3 * time.Minute)
	maxConcurrency   = ptr.To(15)
)

// childReconcileConcurrencyLimit bounds concurrent errgroup fan-out over sibling CRs within a single parent reconcile.
const childReconcileConcurrencyLimit = 5

var (
	optionsInit    sync.Once
	defaultOptions *controller.Options
)

var (
	// TODO: remove parseObjectErrorsTotal, getObjectsErrorsTotal, conflictErrorsTotal, contextCancelErrorsTotal after release 0.80.0
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
	contextCancelErrorsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "operator_controller_reconcile_errors_total",
		Help: "Counts number context.Canceled errors",
	}, []string{"controller", "namespaced_name"})

	controllerErrorsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "operator_controller_errors_total",
		Help: "Counts number controller errors",
	}, []string{"controller", "namespace", "name", "reason"})
	activeConverterWatchers = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "operator_prometheus_converter_active_watchers",
	}, []string{"object_type_name"})
)

// Reasons a reconcile attempt can fail. They double as the "reason" label for controllerErrorsTotal.
const (
	reasonGetObject     = "get_object"
	reasonParseObject   = "parse_object"
	reasonCancelContext = "cancel_context"
	reasonConflict      = "conflict"
	reasonOther         = "other"
)

var legacyCounters = map[string]*prometheus.CounterVec{
	reasonGetObject:     getObjectsErrorsTotal,
	reasonParseObject:   parseObjectErrorsTotal,
	reasonConflict:      conflictErrorsTotal,
	reasonCancelContext: contextCancelErrorsTotal,
}

// InitMetrics adds metrics to the Registry
func init() {
	metrics.Registry.MustRegister(
		parseObjectErrorsTotal, getObjectsErrorsTotal, conflictErrorsTotal,
		contextCancelErrorsTotal, activeConverterWatchers, controllerErrorsTotal)
}

func getDefaultOptions() controller.Options {
	optionsInit.Do(func() {
		defaultOptions = &controller.Options{
			RateLimiter:             workqueue.NewTypedItemExponentialFailureRateLimiter[k8sreconcile.Request](2*time.Second, 2*time.Minute),
			CacheSyncTimeout:        *cacheSyncTimeout,
			MaxConcurrentReconciles: *maxConcurrency,
		}
	})
	return *defaultOptions
}

// ErrShutdown is a custom error returned as a cause of operator context cancel
var ErrShutdown = fmt.Errorf("graceful shutdown, exiting")

// reconcileError wraps a failure that happened while fetching or parsing the reconciled object,
// tagged with reason (reasonGetObject or reasonParseObject) so handleReconcileErr can tell them apart.
type reconcileError struct {
	origin error
	reason string
}

// newGetError wraps a client.Get failure as a reconcileError tagged reasonGetObject.
func newGetError(err error) error {
	return &reconcileError{origin: err, reason: reasonGetObject}
}

// newParsingError wraps a stored ParsingSpecError message as a reconcileError tagged reasonParseObject.
func newParsingError(msg string) error {
	return &reconcileError{origin: errors.New(msg), reason: reasonParseObject}
}

// Unwrap implements errors.Unwrap interface
func (re *reconcileError) Unwrap() error {
	return re.origin
}

func (re *reconcileError) Error() string {
	return fmt.Sprintf("%s error, origin=%q", re.reason, re.origin)
}

func isParsingError(err error) bool {
	var re *reconcileError
	return errors.As(err, &re) && re.reason == reasonParseObject
}

func controllerNameFromObject(object client.Object) string {
	t := reflect.TypeOf(object)
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil {
		return ""
	}
	return strings.ToLower(t.Name())
}

func hasIdentity(object client.Object) bool {
	return object != nil && !reflect.ValueOf(object).IsNil() && object.GetNamespace() != ""
}

func incControllerError(object client.Object, reason string) {
	controller := controllerNameFromObject(object)
	namespace, name := object.GetNamespace(), object.GetName()
	controllerErrorsTotal.WithLabelValues(controller, namespace, name, reason).Inc()
	if legacyCounter, ok := legacyCounters[reason]; ok {
		legacyCounter.WithLabelValues(controller, fmt.Sprintf("%s/%s", namespace, name)).Inc()
	}
}

func handleReconcileErrWithStatus[T client.Object, ST reconcile.StatusWithMetadata[STC], STC any](
	ctx context.Context,
	rclient client.Client,
	object reconcile.ObjectWithDeepCopyAndStatus[T, ST, STC],
	originResult ctrl.Result,
	err error,
) (ctrl.Result, error) {
	result, err := handleReconcileErr(ctx, rclient, object, originResult, err)
	if isParsingError(err) {
		if err := reconcile.UpdateObjectStatus(ctx, rclient, object, vmv1beta1.UpdateStatusFailed, err); err != nil {
			logger.WithContext(ctx).Error(err, "failed to update status with parsing error")
		}
	}
	return result, err
}

// +kubebuilder:rbac:groups="",resources=events,verbs=create

func handleReconcileErr(ctx context.Context, rclient client.Client, object client.Object, originResult ctrl.Result, err error) (ctrl.Result, error) {
	if err == nil || !hasIdentity(object) {
		return originResult, err
	}

	var re *reconcileError

	switch {
	case errors.As(err, &re):
		if re.reason == reasonGetObject {
			deregisterObjectByCollector(object.GetName(), object.GetNamespace(), controllerNameFromObject(object))
			if k8serrors.IsNotFound(err) {
				return originResult, nil
			}
		}
		incControllerError(object, re.reason)
	case errors.Is(err, context.Canceled):
		if errors.Is(context.Cause(ctx), ErrShutdown) {
			return originResult, nil
		}
		incControllerError(object, reasonCancelContext)
		originResult.RequeueAfter = time.Second * 5
		return originResult, nil
	case k8serrors.IsConflict(err):
		incControllerError(object, reasonConflict)
		originResult.RequeueAfter = time.Second * 5
		return originResult, nil
	default:
		incControllerError(object, reasonOther)
	}

	// object is guaranteed to have identity here: the early return above already filtered out objects without one.
	errEvent := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "victoria-metrics-operator-" + uuid.New().String(),
			Namespace: object.GetNamespace(),
			Annotations: map[string]string{
				"operator.victoriametrics.com/controller": controllerNameFromObject(object),
			},
		},
		Type:    corev1.EventTypeWarning,
		Reason:  "ReconciliationError",
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
	return originResult, err
}

func isNamespaceSelectorMatches(ctx context.Context, rclient client.Client, sourceCRD, targetCRD client.Object, selector *metav1.LabelSelector, watchNamespaces []string) (bool, error) {
	switch {
	case selector == nil:
		if sourceCRD.GetNamespace() == targetCRD.GetNamespace() {
			return true, nil
		}
		return false, nil
	case len(selector.MatchLabels) == 0 && len(selector.MatchExpressions) == 0:
		return true, nil
	case len(watchNamespaces) > 0:
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

// isSelectorsMatchesTargetCRD checks if targetCRD matches sourceCRD by entity selectors and selectAll.
// see https://docs.victoriametrics.com/operator/resources/vmagent/#scraping for details
func isSelectorsMatchesTargetCRD(ctx context.Context, rclient client.Client, sourceCRD, targetCRD client.Object, opts *k8stools.SelectorOpts, watchNamespaces []string) (bool, error) {
	// selectAll only works when opts.NamespaceSelector and opts.ObjectSelector opts are undefined
	if opts == nil || (opts.ObjectSelector == nil && opts.NamespaceSelector == nil) {
		return opts.SelectAll, nil
	}
	// check opts.NamespaceSelector, only return when NS not match
	if isNsMatch, err := isNamespaceSelectorMatches(ctx, rclient, sourceCRD, targetCRD, opts.NamespaceSelector, watchNamespaces); !isNsMatch || err != nil {
		return isNsMatch, err
	}
	// in case of empty namespace object must be synchronized in any way,
	// coz we dont know source labels.
	// probably object already deleted.
	if sourceCRD.GetNamespace() == "" {
		return true, nil
	}

	// filter selector label.
	if opts.ObjectSelector == nil {
		return true, nil
	}

	labelSelector, err := metav1.LabelSelectorAsSelector(opts.ObjectSelector)
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

type objectWithStatusTrack[T client.Object, ST reconcile.StatusWithMetadata[STC], STC any] interface {
	client.Object
	LastSpecUpdated() bool
	reconcile.ObjectWithDeepCopyAndStatus[T, ST, STC]
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

func reconcileAndTrackStatus[T client.Object, ST reconcile.StatusWithMetadata[STC], STC any](
	ctx context.Context,
	c client.Client,
	object objectWithStatusTrack[T, ST, STC],
	controllerName string,
	cb func() (ctrl.Result, error),
) (result ctrl.Result, resultErr error) {
	if object.Paused() {
		if err := reconcile.UpdateObjectStatus(ctx, c, object, vmv1beta1.UpdateStatusPaused, nil); err != nil {
			resultErr = fmt.Errorf("failed to update object status: %w", err)
			return
		}
		RegisterObjectStatus(object, controllerName, vmv1beta1.UpdateStatusPaused)
		return
	}
	specChanged := object.LastSpecUpdated()
	resultStatus := vmv1beta1.UpdateStatusOperational
	defer func() {
		if err := reconcile.UpdateObjectStatus(ctx, c, object, resultStatus, resultErr); err != nil {
			resultErr = fmt.Errorf("failed to update object status: %w", err)
			return
		}
		RegisterObjectStatus(object, controllerName, resultStatus)
	}()

	if specChanged {
		if err := reconcile.UpdateObjectStatus(ctx, c, object, vmv1beta1.UpdateStatusExpanding, nil); err != nil {
			resultErr = fmt.Errorf("failed to update object status: %w", err)
			return
		}
		if err := createGenericEventForObject(ctx, c, object, "starting object update"); err != nil {
			logger.WithContext(ctx).Error(err, " cannot create k8s api event")
		}
		logger.WithContext(ctx).Info("object has changes with previous state, applying changes")
	}

	var err error
	result, err = cb()
	if err != nil {
		// do not change status on conflict to failed
		// it should be retried on the next loop
		resultStatus = vmv1beta1.UpdateStatusFailed
		if reconcile.IsRetryable(err) {
			resultStatus = vmv1beta1.UpdateStatusExpanding
		}
		resultErr = err
		return
	}
	if specChanged {
		if err := createGenericEventForObject(ctx, c, object, "reconcile of object finished successfully"); err != nil {
			logger.WithContext(ctx).Error(err, " cannot create k8s api event")
		}
		logger.WithContext(ctx).Info("object was successfully reconciled")
	}
	return result, nil
}
