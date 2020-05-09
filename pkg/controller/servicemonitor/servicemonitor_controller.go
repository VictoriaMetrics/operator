package servicemonitor

import (
	"context"
	"github.com/VictoriaMetrics/operator/conf"
	monitoringv1beta1 "github.com/VictoriaMetrics/operator/pkg/apis/monitoring/v1beta1"
	"github.com/VictoriaMetrics/operator/pkg/controller/factory"
	"github.com/go-logr/logr"
	"k8s.io/client-go/kubernetes"

	monitoringv1 "github.com/VictoriaMetrics/operator/pkg/apis/monitoring/v1"
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

var log = logf.Log.WithName("controller_servicemonitor")

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new ServiceMonitor Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileServiceMonitor{
		client:  mgr.GetClient(),
		scheme:  mgr.GetScheme(),
		kclient: kubernetes.NewForConfigOrDie(mgr.GetConfig()),
		l:       log,
		opConf:  conf.MustGetBaseConfig(),
	}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("servicemonitor-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource ServiceMonitor
	err = c.Watch(&source.Kind{Type: &monitoringv1.ServiceMonitor{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// TODO(user): Modify this to be the types you create that are owned by the primary resource
	// Watch for changes to secondary resource Pods and requeue the owner ServiceMonitor
	err = c.Watch(&source.Kind{Type: &corev1.Pod{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &monitoringv1.ServiceMonitor{},
	})
	if err != nil {
		return err
	}

	return nil
}

// blank assignment to verify that ReconcileServiceMonitor implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileServiceMonitor{}

// ReconcileServiceMonitor reconciles a ServiceMonitor object
type ReconcileServiceMonitor struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client  client.Client
	l       logr.Logger
	kclient kubernetes.Interface
	scheme  *runtime.Scheme
	opConf  *conf.BaseOperatorConf
}

// Reconcile reads that state of the cluster for a ServiceMonitor object and makes changes based on the state read
// and what is in the ServiceMonitor.Spec
// TODO(user): Modify this Reconcile function to implement your Controller logic.  This example creates
// a Pod as an example
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileServiceMonitor) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling ServiceMonitor")

	// Fetch the ServiceMonitor instance
	instance := &monitoringv1.ServiceMonitor{}
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
	var currentVmagent *monitoringv1beta1.VmAgent
	reqLogger.Info("found vmagent objects ", "len: ", len(vmAgentInstances.Items))
	switch len(vmAgentInstances.Items) {
	case 0:
		reqLogger.Info("cannot find vmagent instance, nothing todo")
		return reconcile.Result{}, nil
	case 1:
		currentVmagent = &vmAgentInstances.Items[0]
	default:
		reqLogger.Info("more then 1 mvmagent instances found, we have  to guess TODO")
		return reconcile.Result{}, nil
	}
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
	reqLogger.Info("update service monitor crds")
	return reconcile.Result{}, nil
}
