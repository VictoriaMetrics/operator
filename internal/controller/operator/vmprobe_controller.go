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
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

// VMProbeReconciler reconciles a VMProbe object
type VMProbeReconciler struct {
	client.Client
	Log          logr.Logger
	OriginScheme *runtime.Scheme
	BaseConf     *config.BaseOperatorConf
}

// Init implements crdController interface
func (r *VMProbeReconciler) Init(rclient client.Client, l logr.Logger, sc *runtime.Scheme, cf *config.BaseOperatorConf) {
	r.Client = rclient
	r.Log = l.WithName("controller.VMProbe")
	r.OriginScheme = sc
	r.BaseConf = cf
}

// Scheme implements interface.
func (r *VMProbeReconciler) Scheme() *runtime.Scheme {
	return r.OriginScheme
}

// Reconcile - syncs VMProbe
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmprobes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmprobes/status,verbs=get;update;patch
func (r *VMProbeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	l := r.Log.WithValues("vmprobe", req.Name, "namespace", req.Namespace)
	if build.IsControllerDisabled("VMAgent") && build.IsControllerDisabled("VMSingle") {
		l.Info("skipping VMProbe reconcile since VMAgent and VMSingle controllers are disabled")
		return
	}
	instance := &vmv1beta1.VMProbe{}
	ctx = logger.AddToContext(ctx, l)
	defer func() {
		result, err = handleReconcileErrWithoutStatus(ctx, r.Client, instance, result, err)
	}()

	// Fetch the VMPodScrape instance
	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		return result, &getError{err, "vmprobescrape", req}
	}

	RegisterObjectStat(instance, "vmprobescrape")
	if instance.Spec.ParsingError != "" {
		return result, &parsingError{instance.Spec.ParsingError, "vmprobescrape"}
	}
	if err = collectVMAgentScrapes(l, ctx, r.Client, r.BaseConf.WatchNamespaces, instance); err != nil {
		return
	}
	if err = collectVMSingleScrapes(l, ctx, r.Client, r.BaseConf.WatchNamespaces, instance); err != nil {
		return
	}
	return
}

// SetupWithManager - setups VMProbe manager
func (r *VMProbeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&vmv1beta1.VMProbe{}).
		WithEventFilter(predicate.TypedGenerationChangedPredicate[client.Object]{}).
		WithOptions(getDefaultOptions()).
		Complete(r)
}
