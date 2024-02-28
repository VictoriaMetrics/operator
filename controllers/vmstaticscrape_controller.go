package controllers

import (
	"context"

	"github.com/VictoriaMetrics/operator/controllers/factory"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
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
	instance := &victoriametricsv1beta1.VMStaticScrape{}
	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		return handleGetError(req, "vmstaticscrape", err)
	}
	RegisterObjectStat(instance, "vmstaticscrape")
	if vmAgentReconcileLimit.MustThrottleReconcile() {
		// fast path, rate limited
		return ctrl.Result{}, nil
	}
	vmAgentSync.Lock()
	defer vmAgentSync.Unlock()

	vmAgentInstances := &victoriametricsv1beta1.VMAgentList{}
	err = r.List(ctx, vmAgentInstances, config.MustGetNamespaceListOptions())
	if err != nil {
		return ctrl.Result{}, err
	}

	for _, vmagent := range vmAgentInstances.Items {
		if !vmagent.DeletionTimestamp.IsZero() || vmagent.Spec.ParsingError != "" || vmagent.IsUnmanaged() {
			continue
		}
		reqLogger = reqLogger.WithValues("vmagent", vmagent.Name)
		currentVMagent := &vmagent
		match, err := isSelectorsMatches(instance, currentVMagent, currentVMagent.Spec.StaticScrapeSelector)
		if err != nil {
			reqLogger.Error(err, "cannot match vmagent and VMStaticScrape")
			continue
		}
		// fast path
		if !match {
			continue
		}
		reqLogger.Info("reconciling staticscrapes for vmagent")

		if err := factory.CreateOrUpdateVMAgent(ctx, currentVMagent, r, r.BaseConf); err != nil {
			reqLogger.Error(err, "cannot create or update vmagent instance")
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
