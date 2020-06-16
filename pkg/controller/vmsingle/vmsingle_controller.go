package vmsingle

import (
	"context"
	"github.com/VictoriaMetrics/operator/conf"
	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/pkg/apis/victoriametrics/v1beta1"
	"github.com/VictoriaMetrics/operator/pkg/controller/factory"
	"github.com/operator-framework/operator-sdk/pkg/metrics"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/client-go/rest"

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

var log = logf.Log.WithName("controller_vmsingle")

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new VMSingle Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileVMSingle{
		client:   mgr.GetClient(),
		scheme:   mgr.GetScheme(),
		restConf: mgr.GetConfig(),
		opConf:   conf.MustGetBaseConfig(),
	}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("vmsingle-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource VMSingle
	err = c.Watch(&source.Kind{Type: &victoriametricsv1beta1.VMSingle{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// Watch for changes to secondary resource Pods and requeue the owner VMSingle
	err = c.Watch(&source.Kind{Type: &appsv1.Deployment{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &victoriametricsv1beta1.VMSingle{},
	})
	if err != nil {
		return err
	}

	err = c.Watch(&source.Kind{Type: &corev1.Service{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &victoriametricsv1beta1.VMSingle{},
	})
	if err != nil {
		return err
	}

	return nil
}

// blank assignment to verify that ReconcileVMSingle implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileVMSingle{}

// ReconcileVMSingle reconciles a VMSingle object
type ReconcileVMSingle struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client   client.Client
	scheme   *runtime.Scheme
	restConf *rest.Config
	opConf   *conf.BaseOperatorConf
}

// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileVMSingle) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace",
		request.Namespace, "Request.Name", request.Name,
		"object", "vmsingle",
	)
	reqLogger.Info("Reconciling")

	ctx := context.Background()
	instance := &victoriametricsv1beta1.VMSingle{}
	err := r.client.Get(ctx, request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	if instance.Spec.Storage != nil {
		reqLogger.Info("storage specified reconcile it")
		_, err = factory.CreateVMStorage(ctx, instance, r.client, r.opConf)
		if err != nil {
			reqLogger.Error(err, "cannot create pvc")
			return reconcile.Result{}, err
		}
	}
	_, err = factory.CreateOrUpdateVMSingle(ctx, instance, r.client, r.opConf)
	if err != nil {
		reqLogger.Error(err, "cannot create or update vmsingle deployment")
		return reconcile.Result{}, err
	}

	svc, err := factory.CreateOrUpdateVMSingleService(ctx, instance, r.client, r.opConf)
	if err != nil {
		reqLogger.Error(err, "cannot create or update vmsingle service")
		return reconcile.Result{}, err
	}

	//create servicemonitor for object by default
	if !r.opConf.DisabledServiceMonitorCreation {
		_, err = metrics.CreateServiceMonitors(r.restConf, instance.Namespace, []*corev1.Service{svc})
		if err != nil {
			if !errors.IsAlreadyExists(err) {
				reqLogger.Error(err, "cannot create service monitor")
			}
		}
	}

	reqLogger.Info("vmsingle  reconciled")

	return reconcile.Result{}, nil
}
