package vmalert

import (
	"context"
	"github.com/VictoriaMetrics/operator/conf"
	"github.com/VictoriaMetrics/operator/pkg/controller/factory"
	"github.com/operator-framework/operator-sdk/pkg/metrics"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	monitoringv1beta1 "github.com/VictoriaMetrics/operator/pkg/apis/monitoring/v1beta1"
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

var log = logf.Log.WithName("controller_vmalert")

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new VmAlert Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileVmAlert{
		client:   mgr.GetClient(),
		scheme:   mgr.GetScheme(),
		restConf: mgr.GetConfig(),
		kclient:  kubernetes.NewForConfigOrDie(mgr.GetConfig()),
		opConf:   conf.MustGetBaseConfig(),
	}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("vmalert-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	err = c.Watch(&source.Kind{Type: &monitoringv1beta1.VmAlert{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	err = c.Watch(&source.Kind{Type: &appsv1.Deployment{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &monitoringv1beta1.VmAlert{},
	})
	if err != nil {
		return err
	}

	err = c.Watch(&source.Kind{Type: &corev1.Service{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &monitoringv1beta1.VmAlert{},
	})
	if err != nil {
		return err
	}

	return nil
}

// blank assignment to verify that ReconcileVmAlert implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileVmAlert{}

// ReconcileVmAlert reconciles a VmAlert object
type ReconcileVmAlert struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client   client.Client
	scheme   *runtime.Scheme
	restConf *rest.Config
	kclient  kubernetes.Interface
	opConf   *conf.BaseOperatorConf
}

// Reconcile reads that state of the cluster for a VmAlert object and makes changes based on the state read
// and what is in the VmAlert.Spec
func (r *ReconcileVmAlert) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace",
		request.Namespace, "Request.Name", request.Name,
		"object", "vmalert",
	)
	reqLogger.Info("Reconciling")

	// Fetch the VmAlert instance
	instance := &monitoringv1beta1.VmAlert{}
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

	//first of all, we have to create configmaps with prometheus rules or reconcile existing one
	maps, err := factory.CreateOrUpdateRuleConfigMaps(instance, r.kclient, r.client, reqLogger)
	if err != nil {
		reqLogger.Error(err, "cannot create or update cm")
		return reconcile.Result{}, err
	}
	reqLogger.Info("found cmmaps for vmalert", " len ", len(maps), "map names", maps)

	// Define a new Pod object
	recon, err := factory.CreateOrUpdateVmAlert(instance, r.client, r.opConf, maps, reqLogger)
	if err != nil {
		return recon, err
	}

	svc, err := factory.CreateOrUpdateVmAlertService(instance, r.client, r.opConf, reqLogger)
	if err != nil {
		reqLogger.Error(err, "cannot update service")
		return reconcile.Result{}, err
	}

	_, err = metrics.CreateServiceMonitors(r.restConf, instance.Namespace, []*corev1.Service{svc})
	if err != nil {
		if !errors.IsAlreadyExists(err) {
			reqLogger.Error(err, "cannot create service monitor")
		}
	}

	//TODO rule for vmalert

	reqLogger.Info("full reconciled")

	return reconcile.Result{}, nil
}
