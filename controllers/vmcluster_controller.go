package controllers

import (
	"context"
	"time"

	"github.com/VictoriaMetrics/operator/controllers/factory/finalize"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/controllers/factory"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var log = logf.Log.WithName("controller_vmcluster")

// VMClusterReconciler reconciles a VMCluster object
type VMClusterReconciler struct {
	Client       client.Client
	Log          logr.Logger
	OriginScheme *runtime.Scheme
	BaseConf     *config.BaseOperatorConf
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
func (r *VMClusterReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling VMCluster")

	instance := &victoriametricsv1beta1.VMCluster{}
	if err := r.Client.Get(ctx, request.NamespacedName, instance); err != nil {
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	if !instance.DeletionTimestamp.IsZero() {
		if err := finalize.OnVMClusterDelete(ctx, r.Client, instance); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	if err := finalize.AddFinalizer(ctx, r.Client, instance); err != nil {
		return ctrl.Result{}, err
	}

	status, err := factory.CreateOrUpdateVMCluster(ctx, instance, r.Client, config.MustGetBaseConfig())
	if err != nil {
		reqLogger.Error(err, "cannot update or create vmcluster")
		return reconcile.Result{}, err
	}
	if status == victoriametricsv1beta1.ClusterStatusExpanding {
		reqLogger.Info("cluster still expanding requeue request")
		failCnt := instance.Status.UpdateFailCount
		if failCnt > 5 {
			failCnt = 5
		}
		reqLogger.Info("re queuing cluster expanding with back-off", "fail update count", failCnt)
		// add requeue back-off
		return reconcile.Result{
			RequeueAfter: time.Second * time.Duration(failCnt*10),
		}, nil
	}

	reqLogger.Info("cluster was reconciled")

	return reconcile.Result{}, nil
}

// SetupWithManager general setup method
func (r *VMClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&victoriametricsv1beta1.VMCluster{}).
		Owns(&appsv1.Deployment{}).
		Owns(&v1.Service{}).
		Owns(&victoriametricsv1beta1.VMServiceScrape{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&v1.ServiceAccount{}).
		Complete(r)
}
