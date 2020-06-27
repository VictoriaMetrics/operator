package vmagent

import (
	"context"
	"github.com/VictoriaMetrics/operator/conf"
	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/pkg/apis/victoriametrics/v1beta1"
	"github.com/VictoriaMetrics/operator/pkg/controller/factory"
	apps "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
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

// Add creates a new VMAgent Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileVMAgent{
		client:   mgr.GetClient(),
		scheme:   mgr.GetScheme(),
		opConf:   conf.MustGetBaseConfig(),
		restConf: mgr.GetConfig(),
	}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("vmagent-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource VMAgent
	err = c.Watch(&source.Kind{Type: &victoriametricsv1beta1.VMAgent{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// Watch for changes to secondary resource Pods and requeue the owner VMAgent
	err = c.Watch(&source.Kind{Type: &apps.Deployment{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &victoriametricsv1beta1.VMAgent{},
	})
	if err != nil {
		return err
	}

	err = c.Watch(&source.Kind{Type: &corev1.Service{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &victoriametricsv1beta1.VMAgent{},
	})
	if err != nil {
		return err
	}

	err = c.Watch(&source.Kind{Type: &corev1.Secret{}}, &handler.EnqueueRequestForOwner{
		IsController: false,
		OwnerType:    &victoriametricsv1beta1.VMAgent{},
	})
	if err != nil {
		return err
	}

	return nil
}

// blank assignment to verify that ReconcileVMAgent implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileVMAgent{}

// ReconcileVMAgent reconciles a VMAgent object
type ReconcileVMAgent struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client   client.Client
	scheme   *runtime.Scheme
	restConf *rest.Config
	opConf   *conf.BaseOperatorConf
}

// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileVMAgent) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace",
		request.Namespace, "Request.Name", request.Name,
		"object", "vmagent",
	)
	reqLogger.Info("Reconciling")

	// Fetch the VMAgent instance
	instance := &victoriametricsv1beta1.VMAgent{}
	ctx := context.Background()
	err := r.client.Get(ctx, request.NamespacedName, instance)
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

	//create deploy
	reconResult, err := factory.CreateOrUpdateVMAgent(ctx, instance, r.client, r.opConf)
	if err != nil {
		reqLogger.Error(err, "cannot create or update vmagent deploy")
		return reconResult, err
	}

	//create service for monitoring
	svc, err := factory.CreateOrUpdateVMAgentService(ctx, instance, r.client, r.opConf)
	if err != nil {
		reqLogger.Error(err, "cannot create or update vmagent service")
		return reconcile.Result{}, err
	}

	//create vmservicescrape for object by default
	if !r.opConf.DisableSelfServiceMonitorCreation {
		err := factory.CreateVMServiceScrapeFromService(ctx, r.client, svc)
		if err != nil {
			reqLogger.Error(err, "cannot create serviceScrape for vmagent")
		}

	}
	reqLogger.Info("reconciled vmagent")

	return reconcile.Result{}, nil
}
