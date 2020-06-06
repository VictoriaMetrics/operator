package alertmanager

import (
	"context"
	"github.com/VictoriaMetrics/operator/conf"
	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/pkg/apis/victoriametrics/v1beta1"
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
	return &ReconcileAlertmanager{client: mgr.GetClient(), scheme: mgr.GetScheme(), opConf: conf.MustGetBaseConfig(), l: log}

}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("alertmanager-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource Alertmanager
	err = c.Watch(&source.Kind{Type: &victoriametricsv1beta1.Alertmanager{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// Watch for changes to secondary resource Pods and requeue the owner Alertmanager
	err = c.Watch(&source.Kind{Type: &appsv1.StatefulSet{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &victoriametricsv1beta1.Alertmanager{},
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
	l      logr.Logger
}

// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileAlertmanager) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace",
		request.Namespace, "Request.Name", request.Name,
		"object", "alertmanager",
	)

	reqLogger.Info("Reconciling")
	ctx := context.Background()

	instance := &victoriametricsv1beta1.Alertmanager{}
	err := r.client.Get(ctx, request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	_, err = factory.CreateOrUpdateAlertManager(ctx, instance, r.client, r.opConf)
	if err != nil {
		reqLogger.Error(err, "cannot create or update vmalertmanager sts")
		return reconcile.Result{}, err
	}
	_, err = factory.CreateOrUpdateAlertManagerService(ctx, instance, r.client, r.opConf)
	if err != nil {
		reqLogger.Error(err, "cannot create or update vmalertmanager service")
		return reconcile.Result{}, err
	}

	reqLogger.Info("vmalertmanager reconciled")
	return reconcile.Result{}, nil
}
