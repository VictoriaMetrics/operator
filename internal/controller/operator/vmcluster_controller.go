package operator

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/vmcluster"
)

// VMClusterReconciler reconciles a VMCluster object
type VMClusterReconciler struct {
	Client       client.Client
	Log          logr.Logger
	OriginScheme *runtime.Scheme
	BaseConf     *config.BaseOperatorConf
	name         string
}

// Init implements crdController interface
func (r *VMClusterReconciler) Init(name string, rclient client.Client, l logr.Logger, sc *runtime.Scheme, cf *config.BaseOperatorConf) {
	r.name = strings.ToLower(name)
	r.Client = rclient
	r.Log = l.WithName("controller." + name)
	r.OriginScheme = sc
	r.BaseConf = cf
}

// Scheme implements interface.
func (r *VMClusterReconciler) Scheme() *runtime.Scheme {
	return r.OriginScheme
}

// Reconcile general reconcile method for controller
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmclusters/finalizers,verbs=*
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=*
// +kubebuilder:rbac:groups=apps,resources=statefulsets/status,verbs=get;update;patch
func (r *VMClusterReconciler) Reconcile(ctx context.Context, request ctrl.Request) (result ctrl.Result, err error) {
	l := r.Log.WithValues(r.name, request.Name, "namespace", request.Namespace)
	ctx = logger.AddToContext(ctx, l)
	var instance vmv1beta1.VMCluster

	defer func() {
		result, err = handleReconcileErrWithStatus(ctx, r.Client, &instance, result, err)
	}()

	if err = r.Client.Get(ctx, request.NamespacedName, &instance); err != nil {
		err = &getError{err, r.name, request}
		return
	}

	RegisterObjectStat(&instance, r.name)

	if !instance.DeletionTimestamp.IsZero() {
		err = finalize.OnClusterDelete(ctx, r.Client, &instance)
		return
	}
	if instance.Status.ParsingSpecError != "" && !vmv1beta1.HasUnknownFields(instance.Status.ParsingSpecError) {
		err = &parsingError{instance.Status.ParsingSpecError, r.name}
		return
	}
	if err = finalize.AddFinalizer(ctx, r.Client, &instance); err != nil {
		return
	}
	r.Client.Scheme().Default(&instance)

	result, err = reconcileAndTrackStatus(ctx, r.Client, instance.DeepCopy(), r.name, func() (ctrl.Result, error) {
		if err := vmcluster.CreateOrUpdate(ctx, &instance, r.Client); err != nil {
			return result, fmt.Errorf("failed create or update vmcluster: %w", err)
		}
		return result, nil
	})

	if err == nil {
		result.RequeueAfter = r.BaseConf.ResyncAfterDuration()
	}

	return
}

// SetupWithManager general setup method
func (r *VMClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&vmv1beta1.VMCluster{}).
		Owns(&appsv1.Deployment{}).
		Owns(&appsv1.StatefulSet{}).
		WithOptions(getDefaultOptions()).
		Complete(r)
}

// IsDisabled returns true if controller should be disabled
func (*VMClusterReconciler) IsDisabled(_ *config.BaseOperatorConf, _ sets.Set[string]) bool {
	return false
}
