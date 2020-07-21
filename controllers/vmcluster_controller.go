package controllers

import (
	"context"
	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/conf"
	"github.com/VictoriaMetrics/operator/controllers/factory"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"time"
)

var log = logf.Log.WithName("controller_vmcluster")

// VMClusterReconciler reconciles a VMCluster object
type VMClusterReconciler struct {
	Client   client.Client
	Log      logr.Logger
	Scheme   *runtime.Scheme
	BaseConf *conf.BaseOperatorConf
}

// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=*

func (r *VMClusterReconciler) Reconcile(request ctrl.Request) (ctrl.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling VMCluster")
	ctx := context.TODO()
	cluster := &victoriametricsv1beta1.VMCluster{}
	if err := r.Client.Get(ctx, request.NamespacedName, cluster); err != nil {
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	status, err := factory.CreateOrUpdateVMCluster(ctx, cluster, r.Client, conf.MustGetBaseConfig())
	if err != nil {
		reqLogger.Error(err, "cannot update or create vmcluster")
		return reconcile.Result{}, err
	}
	if status == victoriametricsv1beta1.ClusterStatusExpanding {
		reqLogger.Info("cluster still expanding requeue request")
		failCnt := cluster.Status.UpdateFailCount
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

func (r *VMClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&victoriametricsv1beta1.VMCluster{}).
		Owns(&appsv1.Deployment{}).
		Owns(&appsv1.StatefulSet{}).
		Complete(r)
}
