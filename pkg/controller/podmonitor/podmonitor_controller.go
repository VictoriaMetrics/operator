package podmonitor

import (
	"context"
	"github.com/VictoriaMetrics/operator/conf"
	monitoringv1beta1 "github.com/VictoriaMetrics/operator/pkg/apis/monitoring/v1beta1"
	"github.com/VictoriaMetrics/operator/pkg/controller/factory"
	"k8s.io/client-go/kubernetes"

	monitoringv1 "github.com/VictoriaMetrics/operator/pkg/apis/monitoring/v1"
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

var log = logf.Log.WithName("controller_podmonitor")

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new PodMonitor Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcilePodMonitor{client: mgr.GetClient(),
		scheme:  mgr.GetScheme(),
		kclient: kubernetes.NewForConfigOrDie(mgr.GetConfig()),
		opConf:  conf.MustGetBaseConfig()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("podmonitor-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource PodMonitor
	err = c.Watch(&source.Kind{Type: &monitoringv1.PodMonitor{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	return nil
}

// blank assignment to verify that ReconcilePodMonitor implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcilePodMonitor{}

// ReconcilePodMonitor reconciles a PodMonitor object
type ReconcilePodMonitor struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client  client.Client
	kclient kubernetes.Interface
	opConf  *conf.BaseOperatorConf
	scheme  *runtime.Scheme
}

// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcilePodMonitor) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace,
		"Request.Name", request.Name, "object", "podMonitor")
	reqLogger.Info("Reconciling PodMonitor")

	// Fetch the PodMonitor instance
	instance := &monitoringv1.PodMonitor{}
	err := r.client.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
		} else {
			// Error reading the object - requeue the request.
			return reconcile.Result{}, err
		}
	}
	//well we need vmagent instance...
	vmAgentInstances := &monitoringv1beta1.VmAgentList{}
	//
	err = r.client.List(context.TODO(), vmAgentInstances)
	if err != nil {
		reqLogger.Error(err, "cannot list vmagent objects")
		return reconcile.Result{}, err
	}
	reqLogger.Info("found vmagent objects ", "count len: ", len(vmAgentInstances.Items))

	for _, vmagent := range vmAgentInstances.Items {
		reqLogger = reqLogger.WithValues("vmagent", vmagent.Name)
		reqLogger.Info("reconlining podmonitor for vmagent")
		currentVmagent := &vmagent
		err = factory.CreateOrUpdateConfigurationSecret(currentVmagent, r.client, r.kclient, r.opConf, reqLogger)
		if err != nil {
			reqLogger.Error(err, "cannot create or update default secret for vmagent")
			return reconcile.Result{}, err
		}
		//

		recon, err := factory.CreateOrUpdateVmAgent(currentVmagent, r.client, r.opConf, reqLogger)
		if err != nil {
			reqLogger.Error(err, "")
			return recon, err
		}
		reqLogger.Info("reconciled for vmagnet")
	}

	reqLogger.Info("update pod monitor crds")
	return reconcile.Result{}, nil
}
