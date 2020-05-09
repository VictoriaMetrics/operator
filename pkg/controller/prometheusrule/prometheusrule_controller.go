package prometheusrule

import (
	"context"
	"github.com/VictoriaMetrics/operator/conf"
	monitoringv1beta1 "github.com/VictoriaMetrics/operator/pkg/apis/monitoring/v1beta1"
	"github.com/VictoriaMetrics/operator/pkg/controller/factory"
	"github.com/go-logr/logr"
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

var log = logf.Log.WithName("controller_prometheusrule")

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new Prometheusrule Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcilePrometheusrule{
		client:  mgr.GetClient(),
		scheme:  mgr.GetScheme(),
		opConf:  conf.MustGetBaseConfig(),
		l:       log,
		kclient: kubernetes.NewForConfigOrDie(mgr.GetConfig()),
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
	client  client.Client
	kclient kubernetes.Interface
	l       logr.Logger
	scheme  *runtime.Scheme
	opConf  *conf.BaseOperatorConf
}

// Reconcile reads that state of the cluster for a Prometheusrule object and makes changes based on the state read
// and what is in the Prometheusrule.Spec
// TODO(user): Modify this Reconcile function to implement your Controller logic.  This example creates
// a Pod as an example
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcilePrometheusrule) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling Prometheusrule")

	// Fetch the Prometheusrule instance
	instance := &monitoringv1.PrometheusRule{}
	err := r.client.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			//return reconcile.Result{}, nil
		} else {
			// Error reading the object - requeue the request.
			reqLogger.Error(err, "cannot get resource")
			return reconcile.Result{}, err
		}
	}
	reqLogger.Info("get prom rule", "name", instance.Name)

	//what we have to do
	//get vmalert instance
	//how to find it?
	//
	alertMngs := &monitoringv1beta1.VmAlertList{}
	reqLogger.Info("listing vm alerts")
	err = r.client.List(context.TODO(), alertMngs, &client.ListOptions{})
	if err != nil {
		reqLogger.Error(err, "cannot list vmalerts")
		return reconcile.Result{}, err
	}
	currVmAlert := &monitoringv1beta1.VmAlert{}
	reqLogger.Info("current count of vm alerts: ", "len", len(alertMngs.Items))

	switch len(alertMngs.Items) {
	case 0:
		reqLogger.Info("vm alerts wasnt found, nothing to do")
		return reconcile.Result{}, nil
		//nothing to do, no alertmanagers
	case 1:
		//reconcile one
		reqLogger.Info("one vmalert was found", "name", currVmAlert.Name)
		currVmAlert = &alertMngs.Items[0]
	default:
		//more then 1 vmalert
		//we can try match vmalert by rule labels - for namespace and for rules
		//currVmAlert.Spec.RuleNamespaceSelector
		//currVmAlert.Spec.RuleSelector
		//if we found find match - generate config for it
		//TODO
		reqLogger.Info("more then 1 vm alert was found TODO implement match", "len", len(alertMngs.Items))
		return reconcile.Result{Requeue: false}, nil

	}
	reqLogger.Info("updating or creating cm for vmalert")

	maps, err := factory.CreateOrUpdateRuleConfigMaps(currVmAlert, r.kclient, r.client, reqLogger) // r.createOrUpdateRuleConfigMaps(&currVmAlert)
	if err != nil {
		reqLogger.Error(err, "cannot update rules configmaps")
		return reconcile.Result{}, err
	}
	reqLogger.Info("created rules maps count", "count", len(maps))
	//we have to update it...
	_, err = factory.CreateOrUpdateVmAlert(currVmAlert, r.client, r.opConf, maps, reqLogger)
	if err != nil {
		reqLogger.Error(err, "cannot trigger vmalert update, after rules changing")
		return reconcile.Result{}, nil
	}
	reqLogger.Info("prom rules were reconciled")

	return reconcile.Result{}, nil
}
