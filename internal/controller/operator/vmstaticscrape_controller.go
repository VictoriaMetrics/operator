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
)

// VMStaticScrapeReconciler reconciles a VMStaticScrape object
type VMStaticScrapeReconciler struct {
	client.Client
	Log          logr.Logger
	OriginScheme *runtime.Scheme
}

// Init implements crdController interface
func (r *VMStaticScrapeReconciler) Init(rclient client.Client, l logr.Logger, sc *runtime.Scheme, _ *config.BaseOperatorConf) {
	r.Client = rclient
	r.Log = l.WithName("controller.VMStaticScrape")
	r.OriginScheme = sc
}

// Scheme implements interface.
func (r *VMStaticScrapeReconciler) Scheme() *runtime.Scheme {
	return r.OriginScheme
}

// Reconcile implements interface.
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmstaticscrapes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmstaticscrapes/status,verbs=get;update;patch
func (r *VMStaticScrapeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	l := r.Log.WithValues("vmstaticscrape", req.Name, "namespace", req.Namespace)
	instance := &vmv1beta1.VMStaticScrape{}
	defer func() {
		result, err = handleReconcileErrWithoutStatus(ctx, r.Client, instance, result, err)
	}()
	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		return result, &getError{err, "vmstaticscrape", req}
	}
	RegisterObjectStat(instance, "vmstaticscrape")
	if instance.Spec.ParsingError != "" {
		return result, &parsingError{instance.Spec.ParsingError, "vmstaticscrape"}
	}
	if err = collectVMAgentScrapes(l, ctx, r.Client, instance); err != nil {
		return
	}
	if err = collectVMSingleScrapes(l, ctx, r.Client, instance); err != nil {
		return
	}
	return
}

// SetupWithManager setups reconciler.
func (r *VMStaticScrapeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&vmv1beta1.VMStaticScrape{}).
		WithEventFilter(predicate.TypedGenerationChangedPredicate[client.Object]{}).
		WithOptions(getDefaultOptions()).
		Complete(r)
}

// IsDisabled returns true if controller should be disabled
func (*VMStaticScrapeReconciler) IsDisabled(_ *config.BaseOperatorConf, disabledControllers sets.Set[string]) bool {
	return disabledControllers.HasAll("VMAgent", "VMSingle")
}
