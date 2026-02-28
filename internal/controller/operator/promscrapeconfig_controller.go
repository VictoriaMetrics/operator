/*


Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package operator

import (
	"context"

	"github.com/go-logr/logr"
	promv1alpha1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/VictoriaMetrics/operator/internal/config"
	converter "github.com/VictoriaMetrics/operator/internal/controller/operator/factory/converter/v1alpha1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

// PromScrapeConfigReconciler reconciles a Prometheus ScrapeConfig object
type PromScrapeConfigReconciler struct {
	client.Client
	Log          logr.Logger
	OriginScheme *runtime.Scheme
	BaseConf     *config.BaseOperatorConf
}

// Init implements crdController interface
func (r *PromScrapeConfigReconciler) Init(rclient client.Client, l logr.Logger, sc *runtime.Scheme, cf *config.BaseOperatorConf) {
	r.Client = rclient
	r.Log = l.WithName("controller.PromScrapeConfig")
	r.OriginScheme = sc
	r.BaseConf = cf
	activeConverterWatchers.WithLabelValues("scrapeconfig").Add(1)
}

// Scheme implements interface.
func (r *PromScrapeConfigReconciler) Scheme() *runtime.Scheme {
	return r.OriginScheme
}

// Reconcile general reconcile method for controller
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=scrapeconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=scrapeconfigs/status,verbs=get;update;patch
func (r *PromScrapeConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	l := r.Log.WithValues("scrapeconfig", req.Name, "namespace", req.Namespace)
	instance := &promv1alpha1.ScrapeConfig{}
	ctx = logger.AddToContext(ctx, l)

	defer func() {
		result, err = handleReconcileErrWithoutStatus(ctx, r.Client, instance, result, err)
	}()
	// Fetch the PromScrapeConfig instance
	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		return result, &getError{err, "scrapeconfig", req}
	}

	RegisterObjectStat(instance, "scrapeconfig")
	cr, err := converter.ScrapeConfig(ctx, instance, r.BaseConf)
	if err != nil {
		return result, &getError{err, "scrapeconfig", req}
	}
	var owner *metav1.OwnerReference
	if len(cr.OwnerReferences) > 0 {
		owner = &cr.OwnerReferences[0]
	}

	if err = reconcile.VMScrapeConfig(ctx, r.Client, cr, nil, owner, true); err != nil {
		l.Error(err, "failed to reconcile VMScrapeConfig from ScrapeConfig")
	}
	return
}

// SetupWithManager general setup method
func (r *PromScrapeConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&promv1alpha1.ScrapeConfig{}).
		WithOptions(getDefaultOptions()).
		Complete(r)
}

// IsDisabled returns true if controller should be disabled
func (*PromScrapeConfigReconciler) IsDisabled(cfg *config.BaseOperatorConf, disabledControllers sets.Set[string]) bool {
	return disabledControllers.HasAll("VMAgent", "VMSingle") || disabledControllers.Has("VMScrapeConfig") || !cfg.EnabledPrometheusConverter.ScrapeConfig
}
