package controllers

import (
	"context"
	"fmt"
	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/controllers/factory"
	"github.com/VictoriaMetrics/operator/controllers/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/go-logr/logr"
	"github.com/go-test/deep"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	policyv1beta1 "k8s.io/api/policy/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var log = logf.Log.WithName("controller_vmcluster")

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
func (r *VMClusterReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling VMCluster")

	instance := &victoriametricsv1beta1.VMCluster{}
	err := r.Client.Get(ctx, request.NamespacedName, instance)
	if err != nil {
		return handleGetError(request, "vmcluster", err)
	}

	if !instance.DeletionTimestamp.IsZero() {
		if err := finalize.OnVMClusterDelete(ctx, r.Client, instance); err != nil {
			return ctrl.Result{}, err
		}
		DeregisterObject(instance.Name, instance.Namespace, "vmcluster")
		return ctrl.Result{}, nil
	}

	lastAppliedClusterSpec, err := instance.GetLastAppliedSpec()
	if err != nil {
		reqLogger.Error(err, "cannot parse last applied cluster spec")
	}
	clusterChanges := deep.Equal(lastAppliedClusterSpec, &instance.Spec)
	if len(clusterChanges) > 0 && instance.Status.ClusterStatus != victoriametricsv1beta1.ClusterStatusFailed {
		instance.Status.ClusterStatus = victoriametricsv1beta1.ClusterStatusExpanding
		if err := r.Client.Status().Update(ctx, instance); err != nil {
			return ctrl.Result{}, fmt.Errorf("cannot set expanding status for cluster: %w", err)
		}
	}

	RegisterObject(instance.Name, instance.Namespace, "vmcluster")

	if err := finalize.AddFinalizer(ctx, r.Client, instance); err != nil {
		return ctrl.Result{}, err
	}

	err = factory.CreateOrUpdateVMCluster(ctx, instance, r.Client, config.MustGetBaseConfig())
	if err != nil {
		instance.Status.Reason = err.Error()
		instance.Status.ClusterStatus = victoriametricsv1beta1.ClusterStatusFailed
		if err := r.Client.Status().Update(ctx, instance); err != nil {
			log.Error(err, "cannot update cluster status field")
		}
		// update status
		return reconcile.Result{}, fmt.Errorf("failed create or update vmcluster: %w", err)
	}

	instance.Status.Reason = ""
	instance.Status.ClusterStatus = victoriametricsv1beta1.ClusterStatusOperational
	if err := r.Client.Status().Update(ctx, instance); err != nil {
		return ctrl.Result{}, fmt.Errorf("cannot update cluster status : %w", err)
	}

	if len(clusterChanges) > 0 {
		specPatch, err := instance.LastAppliedSpecAsPatch()
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("cannot parse last applied spec for cluster: %w", err)
		}
		// use patch instead of update, only 1 field must be changed.
		if err := r.Client.Patch(ctx, instance, specPatch); err != nil {
			reqLogger.Error(err, "cannot update cluster object")
			return ctrl.Result{}, fmt.Errorf("cannot update cluster with last applied spec: %w", err)
		}
	}

	reqLogger.Info("cluster was reconciled")

	return reconcile.Result{}, nil
}

// SetupWithManager general setup method
func (r *VMClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&victoriametricsv1beta1.VMCluster{}).
		Owns(&appsv1.Deployment{}, builder.OnlyMetadata).
		Owns(&v1.Service{}, builder.OnlyMetadata).
		Owns(&victoriametricsv1beta1.VMServiceScrape{}, builder.OnlyMetadata).
		Owns(&appsv1.StatefulSet{}, builder.OnlyMetadata).
		Owns(&v1.ServiceAccount{}, builder.OnlyMetadata).
		Owns(&policyv1beta1.PodDisruptionBudget{}).
		WithOptions(defaultOptions).
		Complete(r)
}
