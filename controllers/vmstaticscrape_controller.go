package controllers

import (
	"context"
	"fmt"

	"github.com/VictoriaMetrics/operator/controllers/factory"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

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
func (r *VMStaticScrapeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	reqLogger := r.Log.WithValues("vmservicescrape", req.NamespacedName)
	reqLogger.Info("Reconciling VMStaticScrape")
	// Fetch the VMServiceScrape instance
	instance := &victoriametricsv1beta1.VMStaticScrape{}
	err := r.Get(ctx, req.NamespacedName, instance)
	if err != nil {
		//in case of object notfound we must update vmagents
		if !errors.IsNotFound(err) {
			// Error reading the object - requeue the request.
			reqLogger.Error(err, "cannot get staticScrape")
			return ctrl.Result{}, err
		}
	}
	vmAgentInstances := &victoriametricsv1beta1.VMAgentList{}
	err = r.List(ctx, vmAgentInstances)
	if err != nil {
		reqLogger.Error(err, "cannot list vmagent objects")
		return ctrl.Result{}, err
	}
	reqLogger.Info("found vmagent objects ", "len: ", len(vmAgentInstances.Items))

	for _, vmagent := range vmAgentInstances.Items {
		if vmagent.DeletionTimestamp != nil {
			continue
		}
		reqLogger = reqLogger.WithValues("vmagent", vmagent.Name)
		currentVMagent := &vmagent
		match, err := isVMAgentMatchesVMStaticScrape(currentVMagent, instance)
		if err != nil {
			reqLogger.Error(err, "cannot match vmagent and VMStaticScrape")
			continue
		}
		// fast path
		if !match {
			continue
		}
		reqLogger.Info("reconciling staticscrapes for vmagent")

		recon, err := factory.CreateOrUpdateVMAgent(ctx, currentVMagent, r, r.BaseConf)
		if err != nil {
			reqLogger.Error(err, "cannot create or update vmagent instance")
			return recon, err
		}
		reqLogger.Info("reconciled vmagent")
	}

	reqLogger.Info("reconciled VMStaticScrape")
	return ctrl.Result{}, nil
}

// SetupWithManager setups reconciler.
func (r *VMStaticScrapeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&victoriametricsv1beta1.VMStaticScrape{}).
		Complete(r)
}

// heuristic for selector match.
func isVMAgentMatchesVMStaticScrape(currentVMAgent *victoriametricsv1beta1.VMAgent, vmStaticScrape *victoriametricsv1beta1.VMStaticScrape) (bool, error) {
	// fast path
	if currentVMAgent.Spec.ServiceScrapeNamespaceSelector == nil && currentVMAgent.Namespace != vmStaticScrape.Namespace {
		return false, nil
	}
	// fast path config unmanaged
	if currentVMAgent.Spec.StaticScrapeSelector == nil && currentVMAgent.Spec.StaticScrapeNamespaceSelector == nil {
		return false, nil
	}
	// fast path maybe namespace selector will match.
	if currentVMAgent.Spec.StaticScrapeSelector == nil {
		return true, nil
	}
	selector, err := v1.LabelSelectorAsSelector(currentVMAgent.Spec.ServiceScrapeSelector)
	if err != nil {
		return false, fmt.Errorf("cannot parse vmagent's StaticScrapeSelector selector as labelSelector: %w", err)
	}
	set := labels.Set(vmStaticScrape.Labels)
	// selector not match
	if !selector.Matches(set) {
		return false, nil
	}
	return true, nil
}
