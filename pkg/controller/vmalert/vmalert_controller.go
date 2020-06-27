package vmalert

import (
	"context"
	"github.com/VictoriaMetrics/operator/conf"
	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/pkg/apis/victoriametrics/v1beta1"
	"github.com/VictoriaMetrics/operator/pkg/controller/factory"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"

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

// Add creates a new VMAlert Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileVMAlert{
		client:   mgr.GetClient(),
		scheme:   mgr.GetScheme(),
		restConf: mgr.GetConfig(),
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

	err = c.Watch(&source.Kind{Type: &victoriametricsv1beta1.VMAlert{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	err = c.Watch(&source.Kind{Type: &appsv1.Deployment{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &victoriametricsv1beta1.VMAlert{},
	})
	if err != nil {
		return err
	}

	err = c.Watch(&source.Kind{Type: &corev1.Service{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &victoriametricsv1beta1.VMAlert{},
	})
	if err != nil {
		return err
	}

	return nil
}

// blank assignment to verify that ReconcileVMAlert implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileVMAlert{}

// ReconcileVMAlert reconciles a VMAlert object
type ReconcileVMAlert struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client   client.Client
	scheme   *runtime.Scheme
	restConf *rest.Config
	opConf   *conf.BaseOperatorConf
}

// Reconcile reads that state of the cluster for a VMAlert object and makes changes based on the state read
// and what is in the VMAlert.Spec
func (r *ReconcileVMAlert) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace",
		request.Namespace, "Request.Name", request.Name,
		"object", "vmalert",
	)
	reqLogger.Info("Reconciling")

	// Fetch the VMAlert instance
	ctx := context.Background()
	instance := &victoriametricsv1beta1.VMAlert{}
	err := r.client.Get(ctx, request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	maps, err := factory.CreateOrUpdateRuleConfigMaps(ctx, instance, r.client)
	if err != nil {
		reqLogger.Error(err, "cannot create or update vmalert cm")
		return reconcile.Result{}, err
	}
	reqLogger.Info("found configmaps for vmalert", " len ", len(maps), "map names", maps)

	recon, err := factory.CreateOrUpdateVMAlert(ctx, instance, r.client, r.opConf, maps)
	if err != nil {
		reqLogger.Error(err, "cannot create or update vmalert deploy")
		return recon, err
	}

	svc, err := factory.CreateOrUpdateVMAlertService(ctx, instance, r.client, r.opConf)
	if err != nil {
		reqLogger.Error(err, "cannot create or update update  vmalert service")
		return reconcile.Result{}, err
	}

	//create vmservicescrape for object by default
	if !r.opConf.DisableSelfServiceMonitorCreation {
		err := factory.CreateVMServiceScrapeFromService(ctx, r.client, svc)
		if err != nil {
			reqLogger.Error(err, "cannot create serviceScrape for vmalert")
		}
	}

	reqLogger.Info("vmalert reconciled")

	return reconcile.Result{}, nil
}
