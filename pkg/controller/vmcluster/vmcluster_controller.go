package vmcluster

import (
	"context"
	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/pkg/apis/victoriametrics/v1beta1"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("controller_vmcluster")
var emptyReconcile = reconcile.Result{}

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new VmCluster Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileVmCluster{client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	c, err := controller.New("vmcluster-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}
	if err = c.Watch(&source.Kind{Type: &victoriametricsv1beta1.VmCluster{}}, &handler.EnqueueRequestForObject{}); err != nil {
		return err
	}
	for _, s := range []runtime.Object{&v1.Deployment{}, &v1.StatefulSet{}, &corev1.Service{}} {
		if err = c.Watch(&source.Kind{Type: s}, &handler.EnqueueRequestForOwner{
			IsController: true,
			OwnerType:    &victoriametricsv1beta1.VmCluster{},
		}); err != nil {
			return err
		}
	}
	return nil
}

// blank assignment to verify that ReconcileVmCluster implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileVmCluster{}

// ReconcileVmCluster reconciles a VmCluster object
type ReconcileVmCluster struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
}

type vmController struct {
	client  client.Client
	cluster *victoriametricsv1beta1.VmCluster
}

// Reconcile reads that state of the cluster for a VmCluster object and makes changes based on the state read
// and what is in the VmCluster.Spec
// a Pod as an example
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileVmCluster) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling VmCluster")
	ctx := context.TODO()
	cluster := &victoriametricsv1beta1.VmCluster{}
	if err := r.client.Get(ctx, request.NamespacedName, cluster); err != nil {
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}
	if err := cluster.Validate(); err != nil {
		return reconcile.Result{}, err
	}
	ctl := &vmController{client: r.client, cluster: cluster}
	if r, err := ctl.reconcileStorageLoop(ctx); err != nil || r != emptyReconcile {
		return r, err
	}
	return reconcile.Result{}, nil
}
