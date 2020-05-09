package alertmanager

import (
	"context"
	"github.com/VictoriaMetrics/operator/conf"
	monitoringv1 "github.com/VictoriaMetrics/operator/pkg/apis/monitoring/v1"
	"github.com/VictoriaMetrics/operator/pkg/controller/factory"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
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

var log = logf.Log.WithName("controller_alertmanager")

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new Alertmanager Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileAlertmanager{client: mgr.GetClient(), scheme: mgr.GetScheme(),opConf:conf.MustGetBaseConfig(),l: log}

}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("alertmanager-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource Alertmanager
	err = c.Watch(&source.Kind{Type: &monitoringv1.Alertmanager{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// TODO(user): Modify this to be the types you create that are owned by the primary resource
	// Watch for changes to secondary resource Pods and requeue the owner Alertmanager
	err = c.Watch(&source.Kind{Type: &appsv1.StatefulSet{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &monitoringv1.Alertmanager{},
	})
	if err != nil {
		return err
	}

	return nil
}

// blank assignment to verify that ReconcileAlertmanager implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileAlertmanager{}

// ReconcileAlertmanager reconciles a Alertmanager object
type ReconcileAlertmanager struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
	opConf *conf.BaseOperatorConf
	l logr.Logger
}

// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileAlertmanager) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace",
		request.Namespace, "Request.Name", request.Name,
		"reconcile","alertmanager",
	)

	reqLogger.Info("Reconciling")

	// Fetch the Alertmanager instance
	instance := &monitoringv1.Alertmanager{}

	err := r.client.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	_, err = factory.CreateOrUpdateAlertManager(instance, r.client, r.opConf, reqLogger)
	if err != nil {
		reqLogger.Error(err,"problem with reconiling alertmanager")
		return reconcile.Result{},err
	}
	reqLogger.Info("reconciling service for sts")
    //recon service for alertmanager
	_, err = factory.CreateOrUpdateAlertManagerService(instance, r.client, r.opConf, reqLogger)
	if err != nil {
		return reconcile.Result{},err
	}

	reqLogger.Info("alertmanager reconciled")
	return reconcile.Result{}, nil
}

