package podscrape

import (
	"context"
	"github.com/VictoriaMetrics/operator/conf"
	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/pkg/apis/victoriametrics/v1beta1"
	"github.com/VictoriaMetrics/operator/pkg/controller/factory"
	"k8s.io/client-go/kubernetes"

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

var log = logf.Log.WithName("controller_podscrape")

// Add creates a new VMPodScrape Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileVMPodScrape{client: mgr.GetClient(),
		scheme:  mgr.GetScheme(),
		kclient: kubernetes.NewForConfigOrDie(mgr.GetConfig()),
		opConf:  conf.MustGetBaseConfig()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("podscrape-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource VMPodScrape
	err = c.Watch(&source.Kind{Type: &victoriametricsv1beta1.VMPodScrape{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	return nil
}

// blank assignment to verify that ReconcileVMPodScrape implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileVMPodScrape{}

// ReconcileVMPodScrape reconciles a VMPodScrape object
type ReconcileVMPodScrape struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client  client.Client
	kclient kubernetes.Interface
	opConf  *conf.BaseOperatorConf
	scheme  *runtime.Scheme
}

// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileVMPodScrape) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace,
		"Request.Name", request.Name, "object", "VMPodScrape")
	reqLogger.Info("Reconciling VMPodScrape")

	// Fetch the VMPodScrape instance
	instance := &victoriametricsv1beta1.VMPodScrape{}
	ctx := context.Background()
	err := r.client.Get(ctx, request.NamespacedName, instance)
	if err != nil {
		//in case of object notfound we must update vmagents
		if !errors.IsNotFound(err) {
			// Error reading the object - requeue the request.
			return reconcile.Result{}, err
		}
	}
	vmAgentInstances := &victoriametricsv1beta1.VMAgentList{}
	err = r.client.List(ctx, vmAgentInstances)
	if err != nil {
		reqLogger.Error(err, "cannot list vmagent objects")
		return reconcile.Result{}, err
	}
	reqLogger.Info("found vmagent objects ", "vmagents count: ", len(vmAgentInstances.Items))

	for _, vmagent := range vmAgentInstances.Items {
		reqLogger = reqLogger.WithValues("vmagent", vmagent.Name)
		reqLogger.Info("reconciling podscrape for vmagent")
		currentVMagent := &vmagent
		recon, err := factory.CreateOrUpdateVMAgent(ctx, currentVMagent, r.client, r.opConf)
		if err != nil {
			reqLogger.Error(err, "cannot create or update vmagent")
			return recon, err
		}
		reqLogger.Info("reconciled vmagent")
	}

	reqLogger.Info("reconciled pod monitor")
	return reconcile.Result{}, nil
}
