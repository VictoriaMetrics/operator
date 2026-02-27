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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

// VMPodScrapeReconciler reconciles a VMPodScrape object
type VMPodScrapeReconciler struct {
	client.Client
	Log          logr.Logger
	OriginScheme *runtime.Scheme
	BaseConf     *config.BaseOperatorConf
}

// Init implements crdController interface
func (r *VMPodScrapeReconciler) Init(rclient client.Client, l logr.Logger, sc *runtime.Scheme, cf *config.BaseOperatorConf) {
	r.Client = rclient
	r.Log = l.WithName("controller.VMPodScrape")
	r.OriginScheme = sc
	r.BaseConf = cf
}

// Scheme implements interface.
func (r *VMPodScrapeReconciler) Scheme() *runtime.Scheme {
	return r.OriginScheme
}

// Reconcile general reconcile method for controller
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmpodscrapes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmpodscrapes/status,verbs=get;update;patch
func (r *VMPodScrapeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	l := r.Log.WithValues("vmpodscrape", req.Name, "namespace", req.Namespace)
	instance := &vmv1beta1.VMPodScrape{}
	ctx = logger.AddToContext(ctx, l)

	defer func() {
		result, err = handleReconcileErrWithoutStatus(ctx, r.Client, instance, result, err)
	}()
	// Fetch the VMPodScrape instance
	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		return result, &getError{err, "vmpodscrape", req}
	}

	RegisterObjectStat(instance, "vmpodscrape")
	if instance.Spec.ParsingError != "" {
		return result, &parsingError{instance.Spec.ParsingError, "vmpodscrape"}
	}
	if err = collectVMAgentScrapes(l, ctx, r.Client, r.BaseConf.WatchNamespaces, instance); err != nil {
		return
	}
	if err = collectVMSingleScrapes(l, ctx, r.Client, r.BaseConf.WatchNamespaces, instance); err != nil {
		return
	}
	return
}

// SetupWithManager general setup method
func (r *VMPodScrapeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&vmv1beta1.VMPodScrape{}).
		WithEventFilter(predicate.TypedGenerationChangedPredicate[client.Object]{}).
		WithOptions(getDefaultOptions()).
		Complete(r)
}

// IsDisabled returns true if controller should be disabled
func (*VMPodScrapeReconciler) IsDisabled(_ *config.BaseOperatorConf, disabledControllers sets.Set[string]) bool {
	return disabledControllers.HasAll("VMAgent", "VMSingle")
}
