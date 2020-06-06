package prometheusrule

import (
	"context"
	"github.com/VictoriaMetrics/operator/conf"
	monitoringv1 "github.com/VictoriaMetrics/operator/pkg/apis/monitoring/v1"
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

var log = logf.Log.WithName("controller_prometheusrule")

// Add creates a new Prometheusrule Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcilePrometheusrule{
		client: mgr.GetClient(),
		scheme: mgr.GetScheme(),
		opConf: conf.MustGetBaseConfig(),
		l:      log,
	}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("prometheusrule-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource Prometheusrule
	err = c.Watch(&source.Kind{Type: &monitoringv1.PrometheusRule{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	return nil
}

// blank assignment to verify that ReconcilePrometheusrule implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcilePrometheusrule{}

// ReconcilePrometheusrule reconciles a Prometheusrule object
type ReconcilePrometheusrule struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	l      logr.Logger
	scheme *runtime.Scheme
	opConf *conf.BaseOperatorConf
}

// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcilePrometheusrule) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name,
		"object", "prometheusrule")
	reqLogger.Info("Reconciling Prometheusrule")

	// Fetch the Prometheusrule instance
	instance := &monitoringv1.PrometheusRule{}
	ctx := context.Background()
	err := r.client.Get(ctx, request.NamespacedName, instance)
	if err != nil {
		//in case of object notfound we must update vmalerts
		if !errors.IsNotFound(err) {
			reqLogger.Error(err, "cannot get resource")
			return reconcile.Result{}, err
		}
	}

	alertMngs := &victoriametricsv1beta1.VmAlertList{}
	reqLogger.Info("listing vmalerts")
	err = r.client.List(ctx, alertMngs, &client.ListOptions{})
	if err != nil {
		reqLogger.Error(err, "cannot list vmalerts")
		return reconcile.Result{}, err
	}
	reqLogger.Info("current count of vm alerts: ", "len", len(alertMngs.Items))

	reqLogger.Info("updating or creating cm for vmalert")
	for _, vmalert := range alertMngs.Items {
		reqLogger.WithValues("vmalert", vmalert.Name())
		reqLogger.Info("reconciling vmalert rules")
		currVmAlert := &vmalert

		maps, err := factory.CreateOrUpdateRuleConfigMaps(ctx, currVmAlert, r.client)
		if err != nil {
			reqLogger.Error(err, "cannot update rules configmaps")
			return reconcile.Result{}, err
		}
		reqLogger.Info("created rules maps count", "count", len(maps))

		_, err = factory.CreateOrUpdateVmAlert(ctx, currVmAlert, r.client, r.opConf, maps)
		if err != nil {
			reqLogger.Error(err, "cannot trigger vmalert update, after rules changing")
			return reconcile.Result{}, nil
		}
		reqLogger.Info("reconciled vmalert rules")

	}
	reqLogger.Info("prom rules reconciled")

	return reconcile.Result{}, nil
}
