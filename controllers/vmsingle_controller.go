/*


Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"

	"github.com/VictoriaMetrics/operator/controllers/factory/finalize"
	"sigs.k8s.io/controller-runtime/pkg/builder"

	"github.com/VictoriaMetrics/operator/controllers/factory"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
)

// VMSingleReconciler reconciles a VMSingle object
type VMSingleReconciler struct {
	client.Client
	Log          logr.Logger
	OriginScheme *runtime.Scheme
	BaseConf     *config.BaseOperatorConf
}

// Scheme implements interface.
func (r *VMSingleReconciler) Scheme() *runtime.Scheme {
	return r.OriginScheme
}

// Reconcile general reconcile method for controller
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmsingles,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmsingles/finalizers,verbs=*
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=*
// +kubebuilder:rbac:groups=apps,resources=replicasets,verbs=*
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=*
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmsingles/status,verbs=get;update;patch
func (r *VMSingleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	reqLogger := r.Log.WithValues("vmsingle", req.NamespacedName)
	reqLogger.Info("Reconciling vmsingle")

	instance := &victoriametricsv1beta1.VMSingle{}
	err := r.Get(ctx, req.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if !instance.DeletionTimestamp.IsZero() {
		if err := finalize.OnVMSingleDelete(ctx, r.Client, instance); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}
	if err := finalize.AddFinalizer(ctx, r.Client, instance); err != nil {
		return ctrl.Result{}, err
	}

	if instance.Spec.Storage != nil && instance.Spec.StorageDataPath == "" {
		reqLogger.Info("reconciling storage for VMSingle")
		_, err = factory.CreateVMSingleStorage(ctx, instance, r, r.BaseConf)
		if err != nil {
			reqLogger.Error(err, "cannot create pvc")
			return ctrl.Result{}, err
		}
	}
	_, err = factory.CreateOrUpdateVMSingle(ctx, instance, r, r.BaseConf)
	if err != nil {
		reqLogger.Error(err, "cannot create or update VMSingle deployment")
		return ctrl.Result{}, err
	}

	svc, err := factory.CreateOrUpdateVMSingleService(ctx, instance, r, r.BaseConf)
	if err != nil {
		reqLogger.Error(err, "cannot create or update vmsingle service")
		return ctrl.Result{}, err
	}

	if !r.BaseConf.DisableSelfServiceScrapeCreation {
		err := factory.CreateVMServiceScrapeFromService(ctx, r, svc, instance.MetricPath())
		if err != nil {
			reqLogger.Error(err, "cannot create serviceScrape for vmsingle")
		}
	}

	reqLogger.Info("vmsingle  reconciled")
	return ctrl.Result{}, nil
}

// SetupWithManager general setup method
func (r *VMSingleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&victoriametricsv1beta1.VMSingle{}).
		Owns(&appsv1.Deployment{}, builder.OnlyMetadata).
		Owns(&v1.Service{}, builder.OnlyMetadata).
		Owns(&victoriametricsv1beta1.VMServiceScrape{}).
		Owns(&v1.ServiceAccount{}, builder.OnlyMetadata).
		Complete(r)
}
