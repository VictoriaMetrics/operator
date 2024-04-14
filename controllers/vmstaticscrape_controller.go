package controllers

import (
	"context"
	"fmt"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/controllers/factory/k8stools"
	"github.com/VictoriaMetrics/operator/controllers/factory/logger"
	"github.com/VictoriaMetrics/operator/controllers/factory/vmagent"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// VMStaticScrapeReconciler reconciles a VMStaticScrape object
type VMStaticScrapeReconciler struct {
	client.Client
	Log          logr.Logger
	OriginScheme *runtime.Scheme
	BaseConf     *config.BaseOperatorConf
}

// Scheme implements interface.
func (r *VMStaticScrapeReconciler) Scheme() *runtime.Scheme {
	return r.OriginScheme
}

// Reconcile implements interface.
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmstaticscrapes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmstaticscrapes/status,verbs=get;update;patch
func (r *VMStaticScrapeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	reqLogger := r.Log.WithValues("vmstaticscrape", req.NamespacedName)
	defer func() {
		result, err = handleReconcileErr(ctx, r.Client, nil, result, err)
	}()
	instance := &victoriametricsv1beta1.VMStaticScrape{}
	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		return result, &getError{err, "vmstaticscrape", req}
	}
	RegisterObjectStat(instance, "vmstaticscrape")
	if vmAgentReconcileLimit.MustThrottleReconcile() {
		// fast path, rate limited
		return ctrl.Result{}, nil
	}
	vmAgentSync.Lock()
	defer vmAgentSync.Unlock()

	var objects victoriametricsv1beta1.VMAgentList
	if err := k8stools.ListObjectsByNamespace(ctx, r.Client, config.MustGetWatchNamespaces(), func(dst *victoriametricsv1beta1.VMAgentList) {
		objects.Items = append(objects.Items, dst.Items...)
	}); err != nil {
		return result, fmt.Errorf("cannot list vmauths for vmuser: %w", err)
	}

	for _, vmagentItem := range objects.Items {
		if !vmagentItem.DeletionTimestamp.IsZero() || vmagentItem.Spec.ParsingError != "" || vmagentItem.IsUnmanaged() {
			continue
		}
		currentVMagent := &vmagentItem
		if !currentVMagent.Spec.SelectAllByDefault {
			match, err := isSelectorsMatchesTargetCRD(ctx, r.Client, instance, currentVMagent, currentVMagent.Spec.StaticScrapeSelector, currentVMagent.Spec.StaticScrapeNamespaceSelector)
			if err != nil {
				reqLogger.Error(err, "cannot match vmagent and vmStaticScrape")
				continue
			}
			if !match {
				continue
			}
		}
		reqLogger := reqLogger.WithValues("vmagent", currentVMagent.Name)
		ctx := logger.AddToContext(ctx, reqLogger)

		if err := vmagent.CreateOrUpdateConfigurationSecret(ctx, currentVMagent, r, r.BaseConf); err != nil {
			continue
		}
	}
	return
}

// SetupWithManager setups reconciler.
func (r *VMStaticScrapeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&victoriametricsv1beta1.VMStaticScrape{}).
		WithOptions(getDefaultOptions()).
		Complete(r)
}
