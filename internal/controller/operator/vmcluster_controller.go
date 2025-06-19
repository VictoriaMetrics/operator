package operator

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/runtime"
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
}

// Init implements crdController interface
func (r *VMClusterReconciler) Init(rclient client.Client, l logr.Logger, sc *runtime.Scheme, cf *config.BaseOperatorConf) {
	r.Client = rclient
	r.Log = l.WithName("controller.VMCluster")
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
func (r *VMClusterReconciler) Reconcile(ctx context.Context, request ctrl.Request) (result ctrl.Result, err error) {
	l := r.Log.WithValues("vmcluster", request.Name, "namespace", request.Namespace)
	ctx = logger.AddToContext(ctx, l)
	instance := &vmv1beta1.VMCluster{}

	defer func() {
		result, err = handleReconcileErr(ctx, r.Client, instance, result, err)
	}()

	if err := r.Client.Get(ctx, request.NamespacedName, instance); err != nil {
		return result, &getError{err, "vmcluster", request}
	}

	RegisterObjectStat(instance, "vmcluster")

	if !instance.DeletionTimestamp.IsZero() {
		if err := finalize.OnVMClusterDelete(ctx, r.Client, instance); err != nil {
			return result, err
		}
		return result, nil
	}
	if instance.Spec.ParsingError != "" {
		return result, &parsingError{instance.Spec.ParsingError, "vmcluster"}
	}
	if err := finalize.AddFinalizer(ctx, r.Client, instance); err != nil {
		return result, err
	}
	r.Client.Scheme().Default(instance)

	result, err = reconcileAndTrackStatus(ctx, r.Client, instance.DeepCopy(), func() (ctrl.Result, error) {
		err = vmcluster.CreateOrUpdate(ctx, instance, r.Client)
		if err != nil {
			return result, fmt.Errorf("failed create or update vmcluster: %w", err)
		}
		return result, nil
	})
	if err != nil {
		return
	}

	result.RequeueAfter = r.BaseConf.ResyncAfterDuration()
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
