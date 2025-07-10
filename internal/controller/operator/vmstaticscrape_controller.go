package operator

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/vmagent"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/vmsingle"
)

// VMStaticScrapeReconciler reconciles a VMStaticScrape object
type VMStaticScrapeReconciler struct {
	client.Client
	Log          logr.Logger
	OriginScheme *runtime.Scheme
}

// Init implements crdController interface
func (r *VMStaticScrapeReconciler) Init(rclient client.Client, l logr.Logger, sc *runtime.Scheme, cf *config.BaseOperatorConf) {
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
	instance := &vmv1beta1.VMStaticScrape{}
	l := r.Log.WithValues("vmstaticscrape", req.Name, "namespace", req.Namespace)
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
	if !agentReconcileLimit.MustThrottleReconcile() {
		agentSync.Lock()
		var objects vmv1beta1.VMAgentList
		if err := k8stools.ListObjectsByNamespace(ctx, r.Client, config.MustGetWatchNamespaces(), func(dst *vmv1beta1.VMAgentList) {
			objects.Items = append(objects.Items, dst.Items...)
		}); err != nil {
			return result, fmt.Errorf("cannot list VMAgents for VMStaticScrape: %w", err)
		}

		for i := range objects.Items {
			item := &objects.Items[i]
			if !item.DeletionTimestamp.IsZero() || item.Spec.ParsingError != "" || item.IsStaticScrapeUnmanaged() {
				continue
			}
			l := l.WithValues("vmagent", item.Name, "parent_namespace", item.Namespace)
			ctx := logger.AddToContext(ctx, l)
			if item.Spec.DaemonSetMode {
				continue
			}
			// only check selector when deleting object,
			// since labels can be changed when updating and we can't tell if it was selected before, and we can't tell if it's creating or updating.
			if !instance.DeletionTimestamp.IsZero() {
				opts := &k8stools.SelectorOpts{
					SelectAll:         item.Spec.SelectAllByDefault,
					NamespaceSelector: item.Spec.StaticScrapeNamespaceSelector,
					ObjectSelector:    item.Spec.StaticScrapeSelector,
					DefaultNamespace:  instance.Namespace,
				}
				match, err := isSelectorsMatchesTargetCRD(ctx, r.Client, instance, item, opts)
				if err != nil {
					l.Error(err, "cannot match VMAgent and VMStaticScrape")
					continue
				}
				if !match {
					continue
				}
			}

			if err := vmagent.CreateOrUpdateScrapeConfig(ctx, r, item, instance); err != nil {
				continue
			}
		}
		agentSync.Unlock()
	}

	if !vmsingleReconcileLimit.MustThrottleReconcile() {
		vmsingleSync.Lock()

		var objects vmv1beta1.VMSingleList
		if err := k8stools.ListObjectsByNamespace(ctx, r.Client, config.MustGetWatchNamespaces(), func(dst *vmv1beta1.VMSingleList) {
			objects.Items = append(objects.Items, dst.Items...)
		}); err != nil {
			return result, fmt.Errorf("cannot list VMSingles for VMStaticScrape: %w", err)
		}

		for i := range objects.Items {
			item := &objects.Items[i]
			if !item.DeletionTimestamp.IsZero() || item.Spec.ParsingError != "" || item.IsStaticScrapeUnmanaged() {
				continue
			}
			l := l.WithValues("vmsingle", item.Name, "parent_namespace", item.Namespace)
			ctx := logger.AddToContext(ctx, l)
			// only check selector when deleting object,
			// since labels can be changed when updating and we can't tell if it was selected before, and we can't tell if it's creating or updating.
			if !instance.DeletionTimestamp.IsZero() {
				opts := &k8stools.SelectorOpts{
					SelectAll:         item.Spec.SelectAllByDefault,
					NamespaceSelector: item.Spec.StaticScrapeNamespaceSelector,
					ObjectSelector:    item.Spec.StaticScrapeSelector,
					DefaultNamespace:  instance.Namespace,
				}
				match, err := isSelectorsMatchesTargetCRD(ctx, r.Client, instance, item, opts)
				if err != nil {
					l.Error(err, "cannot match VMSingle and VMStaticScrape")
					continue
				}
				if !match {
					continue
				}
			}

			if err := vmsingle.CreateOrUpdateScrapeConfig(ctx, r, item, instance); err != nil {
				continue
			}
		}
		vmsingleSync.Unlock()
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
