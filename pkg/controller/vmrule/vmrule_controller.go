package vmrule

import (
	"context"
	"github.com/VictoriaMetrics/operator/conf"
	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/pkg/apis/victoriametrics/v1beta1"
	"github.com/VictoriaMetrics/operator/pkg/controller/factory"
	"github.com/go-logr/logr"
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

var log = logf.Log.WithName("controller_alertrule")

// Add creates a new VMRule Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileVMRule{
		client: mgr.GetClient(),
		scheme: mgr.GetScheme(),
		opConf: conf.MustGetBaseConfig(),
		l:      log,
	}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("vmrule-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource VMRule
	err = c.Watch(&source.Kind{Type: &victoriametricsv1beta1.VMRule{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	return nil
}

// blank assignment to verify that ReconcileVMRule implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileVMRule{}

// ReconcileVMRule reconciles a VMRule object
type ReconcileVMRule struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	l      logr.Logger
	scheme *runtime.Scheme
	opConf *conf.BaseOperatorConf
}

// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileVMRule) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name,
		"object", "vmrule")
	reqLogger.Info("Reconciling VMRule")

	// Fetch the VMRule instance
	instance := &victoriametricsv1beta1.VMRule{}
	ctx := context.Background()
	err := r.client.Get(ctx, request.NamespacedName, instance)
	if err != nil {
		//in case of object notfound we must update vmalerts
		if !errors.IsNotFound(err) {
			reqLogger.Error(err, "cannot get resource")
			return reconcile.Result{}, err
		}
	}

	alertMngs := &victoriametricsv1beta1.VMAlertList{}
	reqLogger.Info("listing vmalerts")
	err = r.client.List(ctx, alertMngs, &client.ListOptions{})
	if err != nil {
		reqLogger.Error(err, "cannot list vmalerts")
		return reconcile.Result{}, err
	}
	reqLogger.Info("current count of vm alerts: ", "len", len(alertMngs.Items))

	reqLogger.Info("updating or creating cm for vmalert")
	for _, vmalert := range alertMngs.Items {
		reqLogger.WithValues("vmalert", vmalert.Name)
		reqLogger.Info("reconciling vmalert rules")
		currVMAlert := &vmalert

		maps, err := factory.CreateOrUpdateRuleConfigMaps(ctx, currVMAlert, r.client)
		if err != nil {
			reqLogger.Error(err, "cannot update rules configmaps")
			return reconcile.Result{}, err
		}
		reqLogger.Info("created rules maps count", "count", len(maps))

		_, err = factory.CreateOrUpdateVMAlert(ctx, currVMAlert, r.client, r.opConf, maps)
		if err != nil {
			reqLogger.Error(err, "cannot trigger vmalert update, after rules changing")
			return reconcile.Result{}, nil
		}
		reqLogger.Info("reconciled vmalert rules")

	}
	reqLogger.Info("alert rule was reconciled")

	return reconcile.Result{}, nil
}
