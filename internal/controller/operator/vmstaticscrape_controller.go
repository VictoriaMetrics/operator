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
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

// VMStaticScrapeReconciler reconciles a VMStaticScrape object
type VMStaticScrapeReconciler struct {
	client.Client
	Log          logr.Logger
	OriginScheme *runtime.Scheme
	BaseConf     *config.BaseOperatorConf
	name         string
}

// Init implements crdController interface
func (r *VMStaticScrapeReconciler) Init(name string, rclient client.Client, l logr.Logger, sc *runtime.Scheme, cf *config.BaseOperatorConf) {
	r.name = strings.ToLower(name)
	r.Client = rclient
	r.Log = l.WithName("controller." + name)
	r.OriginScheme = sc
	r.BaseConf = cf
}

// Scheme implements interface.
func (r *VMStaticScrapeReconciler) Scheme() *runtime.Scheme {
	return r.OriginScheme
}

// Reconcile implements interface.
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmstaticscrapes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmstaticscrapes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmstaticscrapes/finalizers,verbs=*

func (r *VMStaticScrapeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	l := r.Log.WithValues(r.name, req.Name, "namespace", req.Namespace)
	var instance vmv1beta1.VMStaticScrape
	defer func() {
		result, err = handleReconcileErrWithStatus(ctx, r.Client, &instance, result, err)
	}()
	instance.Name, instance.Namespace = req.Name, req.Namespace
	if err = r.Get(ctx, req.NamespacedName, &instance); err != nil {
		err = newGetError(err)
		return
	}
	RegisterObjectStat(&instance, r.name)
	if instance.Status.ParsingSpecError != "" && !vmv1beta1.HasUnknownFields(instance.Status.ParsingSpecError) {
		err = newParsingError(instance.Status.ParsingSpecError)
		return
	}
	err = collectVMAgentScrapes(l, ctx, r.Client, r.BaseConf, &instance)
	if singleErr := collectVMSingleScrapes(l, ctx, r.Client, r.BaseConf, &instance); singleErr != nil && err == nil {
		err = singleErr
	}
	if err == nil {
		err = reconcile.SyncAggregatedChildStatus(ctx, r.Client, &instance)
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
