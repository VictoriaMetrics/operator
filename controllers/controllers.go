package controllers

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"sync"
	"time"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	cacheSyncTimeout = flag.Duration("controller.cacheSyncTimeout", 3*time.Minute, "controls timeout for caches to be synced.")
	maxConcurrency   = flag.Int("controller.maxConcurrentReconciles", 1, "Configures number of concurrent reconciles. It should improve performance for clusters with many objects.")
)

var (
	optionsInit    sync.Once
	defaultOptions *controller.Options
)

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

var (
	parseObjectErrorsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "operator_controller_object_parsing_errors_total",
		Help: "Counts number of objects, that was failed to parse from json",
	}, []string{"controller"})
	parseObjectErrorsInit = sync.Once{}
)

type objectWithParsingError interface {
	GetObjectKind() schema.ObjectKind
	GetObjectMeta() v1.Object
}

func handleParsingError(parsingErr string, obj objectWithParsingError) (ctrl.Result, error) {
	parseObjectErrorsInit.Do(func() {
		metrics.Registry.MustRegister(parseObjectErrorsTotal)
	})
	kind := strings.ToLower(obj.GetObjectKind().GroupVersionKind().Kind)
	log.Error(fmt.Errorf(parsingErr), "cannot parse object", "name", obj.GetObjectMeta().GetName(), "namespace", obj.GetObjectMeta().GetNamespace(), "controller", kind)
	parseObjectErrorsTotal.WithLabelValues(kind).Inc()
	return ctrl.Result{}, nil
}

func isSelectorsMatches(sourceCRD, targetCRD client.Object, selector *v1.LabelSelector) (bool, error) {
	// in case of empty namespace object must be synchronized in any way,
	// coz we dont know source labels.
	// probably object already deleted.
	if sourceCRD.GetNamespace() == "" || sourceCRD.GetNamespace() == targetCRD.GetNamespace() {
		return true, nil
	}

	// filter selector label.
	if selector == nil {
		return true, nil
	}

	labelSelector, err := v1.LabelSelectorAsSelector(selector)
	if err != nil {
		return false, fmt.Errorf("cannot parse vmalert's RuleSelector selector as labelSelector: %w", err)
	}
	set := labels.Set(sourceCRD.GetLabels())
	// selector not match
	if !labelSelector.Matches(set) {
		return false, nil
	}
	return true, nil
}

func handleGetError(reqObject ctrl.Request, controller string, err error) (ctrl.Result, error) {
	if errors.IsNotFound(err) {
		deregisterObjectByCollector(reqObject.Name, reqObject.Namespace, controller)
		return ctrl.Result{}, nil
	}
	return ctrl.Result{}, err
}

type objectWithStatusTrack interface {
	client.Object
	HasSpecChanges() (bool, error)
	LastAppliedSpecAsPatch() (client.Patch, error)
	SetUpdateStatusTo(ctx context.Context, r client.Client, status victoriametricsv1beta1.UpdateStatus, maybeReason error) error
}

func reconcileAndTrackStatus(ctx context.Context, c client.Client, object objectWithStatusTrack, cb func() (ctrl.Result, error)) (result ctrl.Result, resultErr error) {
	specChanged, err := object.HasSpecChanges()
	if err != nil {
		resultErr = fmt.Errorf("cannot parse exist spec changes")
		return
	}
	if specChanged {
		if err := object.SetUpdateStatusTo(ctx, c, victoriametricsv1beta1.UpdateStatusExpanding, nil); err != nil {
			resultErr = fmt.Errorf("failed to update object status: %w", err)
			return
		}
	}

	result, err = cb()
	if err != nil {
		if updateErr := object.SetUpdateStatusTo(ctx, c, victoriametricsv1beta1.UpdateStatusFailed, err); updateErr != nil {
			resultErr = fmt.Errorf("failed to update object status: %q, origin err: %w", updateErr, err)
			return
		}

		return result, fmt.Errorf("callback error: %w", err)
	}

	if err := object.SetUpdateStatusTo(ctx, c, victoriametricsv1beta1.UpdateStatusOperational, nil); err != nil {
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
	}

	return result, nil
}
