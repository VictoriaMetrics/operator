package controller

import (
	"context"
	"fmt"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/controller/factory/vmcluster"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var log = logf.Log.WithName("controller")

// VMClusterReconciler reconciles a VMCluster object
type VMClusterReconciler struct {
	Client       client.Client
	Log          logr.Logger
	OriginScheme *runtime.Scheme
	BaseConf     *config.BaseOperatorConf
}

// Scheme implements interface.
func (r *VMClusterReconciler) Scheme() *runtime.Scheme {
	return r.OriginScheme
}

// Reconcile general reconcile method for controller
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmclusters/finalizers,verbs=*
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=*
func (r *VMClusterReconciler) Reconcile(ctx context.Context, request ctrl.Request) (result ctrl.Result, err error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	ctx = logger.AddToContext(ctx, reqLogger)
	instance := &vmv1beta1.VMCluster{}

	defer func() {
		result, err = handleReconcileErr(ctx, r.Client, instance, result, err)
	}()

	if err := r.Client.Get(ctx, request.NamespacedName, instance); err != nil {
		return result, &getError{err, "vmcluster", request}
	}

	RegisterObjectStat(instance, "vmcluster")

	if !instance.DeletionTimestamp.IsZero() {
		if err := finalize.OnVMClusterDelete(ctx, r.Client, instance); err != nil {
			return result, err
		}
		return result, nil
	}
	if instance.Spec.ParsingError != "" {
		return result, &parsingError{instance.Spec.ParsingError, "vmcluster"}
	}
	if err := finalize.AddFinalizer(ctx, r.Client, instance); err != nil {
		return result, err
	}
	return reconcileAndTrackStatus(ctx, r.Client, instance, func() (ctrl.Result, error) {
		err = vmcluster.CreateOrUpdateVMCluster(ctx, instance, r.Client, config.MustGetBaseConfig())
		if err != nil {
			return result, fmt.Errorf("failed create or update vmcluster: %w", err)
		}
		return result, nil
	})
}

// SetupWithManager general setup method
func (r *VMClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&vmv1beta1.VMCluster{}).
		Owns(&appsv1.Deployment{}).
		Owns(&appsv1.StatefulSet{}).
		WithOptions(getDefaultOptions()).
		Complete(r)
}
