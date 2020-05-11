package vmagent

import (
	"context"
	"github.com/VictoriaMetrics/operator/conf"
	monitoringv1beta1 "github.com/VictoriaMetrics/operator/pkg/apis/monitoring/v1beta1"
	"github.com/VictoriaMetrics/operator/pkg/controller/factory"
	"github.com/operator-framework/operator-sdk/pkg/metrics"
	apps "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("controller_vmagent")

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new VmAgent Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileVmAgent{
		client:   mgr.GetClient(),
		scheme:   mgr.GetScheme(),
		opConf:   conf.MustGetBaseConfig(),
		restConf: mgr.GetConfig(),
		kclient:  kubernetes.NewForConfigOrDie(mgr.GetConfig()),
	}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("vmagent-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource VmAgent
	err = c.Watch(&source.Kind{Type: &monitoringv1beta1.VmAgent{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// Watch for changes to secondary resource Pods and requeue the owner VmAgent
	err = c.Watch(&source.Kind{Type: &apps.Deployment{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &monitoringv1beta1.VmAgent{},
	})
	if err != nil {
		return err
	}

	err = c.Watch(&source.Kind{Type: &corev1.Service{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &monitoringv1beta1.VmAgent{},
	})
	if err != nil {
		return err
	}

	return nil
}

// blank assignment to verify that ReconcileVmAgent implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileVmAgent{}

// ReconcileVmAgent reconciles a VmAgent object
type ReconcileVmAgent struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client   client.Client
	kclient  kubernetes.Interface
	scheme   *runtime.Scheme
	restConf *rest.Config
	opConf   *conf.BaseOperatorConf
}

// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileVmAgent) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace",
		request.Namespace, "Request.Name", request.Name,
		"object", "vmagent",
	)
	reqLogger.Info("Reconciling")

	// Fetch the VmAgent instance
	instance := &monitoringv1beta1.VmAgent{}
	err := r.client.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		reqLogger.Error(err, "cannot get vmagent object for reconcile")
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	//we have to create empty or full cm first
	err = factory.CreateOrUpdateConfigurationSecret(instance, r.client, r.kclient, r.opConf, reqLogger)
	if err != nil {
		reqLogger.Error(err, "cannot create configmap")
		return reconcile.Result{}, err
	}

	//create deploy
	reconResult, err := factory.CreateOrUpdateVmAgent(instance, r.client, r.opConf, reqLogger)
	if err != nil {
		//propogate err + result to upstream
		return reconResult, err
	}

	//create service for monitoring
	svc, err := factory.CreateOrUpdateVmAgentService(instance, r.client, r.opConf, reqLogger)
	if err != nil {
		return reconcile.Result{}, err
	}

	//cm
	_, err = metrics.CreateServiceMonitors(r.restConf, instance.Namespace, []*corev1.Service{svc})
	if err != nil {
		if !errors.IsAlreadyExists(err) {
			reqLogger.Error(err, "cannot create service monitor")
		}
	}

	//TODO create some rule for vmagent

	return reconcile.Result{}, nil
}
