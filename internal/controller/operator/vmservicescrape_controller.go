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
	"strings"

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

// VMServiceScrapeReconciler reconciles a VMServiceScrape object
type VMServiceScrapeReconciler struct {
	client.Client
	Log          logr.Logger
	OriginScheme *runtime.Scheme
	BaseConf     *config.BaseOperatorConf
	name         string
}

// Init implements crdController interface
func (r *VMServiceScrapeReconciler) Init(name string, rclient client.Client, l logr.Logger, sc *runtime.Scheme, cf *config.BaseOperatorConf) {
	r.name = strings.ToLower(name)
	r.Client = rclient
	r.Log = l.WithName("controller." + name)
	r.OriginScheme = sc
	r.BaseConf = cf
}

// Scheme implements interface.
func (r *VMServiceScrapeReconciler) Scheme() *runtime.Scheme {
	return r.OriginScheme
}

// Reconcile general reconcile method for controller
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmservicescrapes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmservicescrapes/status,verbs=get;update;patch
func (r *VMServiceScrapeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	l := r.Log.WithValues(r.name, req.Name, "namespace", req.Namespace)
	ctx = logger.AddToContext(ctx, l)
	var instance vmv1beta1.VMServiceScrape
	defer func() {
		result, err = handleReconcileErrWithStatus(ctx, r.Client, &instance, result, err)
	}()

	// Fetch the VMServiceScrape instance
	if err = r.Get(ctx, req.NamespacedName, &instance); err != nil {
		err = &getError{err, r.name, req}
		return
	}

	RegisterObjectStat(&instance, r.name)
	if instance.Status.ParsingSpecError != "" && !vmv1beta1.HasUnknownFields(instance.Status.ParsingSpecError) {
		err = &parsingError{instance.Status.ParsingSpecError, r.name}
		return
	}
	if err = collectVMAgentScrapes(l, ctx, r.Client, r.BaseConf.WatchNamespaces, &instance); err != nil {
		return
	}
	if err = collectVMSingleScrapes(l, ctx, r.Client, r.BaseConf.WatchNamespaces, &instance); err != nil {
		return
	}
	return
}

// SetupWithManager general setup method
func (r *VMServiceScrapeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&vmv1beta1.VMServiceScrape{}).
		WithEventFilter(predicate.TypedGenerationChangedPredicate[client.Object]{}).
		WithOptions(getDefaultOptions()).
		Complete(r)
}

// IsDisabled returns true if controller should be disabled
func (*VMServiceScrapeReconciler) IsDisabled(_ *config.BaseOperatorConf, disabledControllers sets.Set[string]) bool {
	return disabledControllers.HasAll("VMAgent", "VMSingle")
}
