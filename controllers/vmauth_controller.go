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
	"sync"

	"github.com/VictoriaMetrics/operator/controllers/factory"
	"github.com/VictoriaMetrics/operator/controllers/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
)

var vmAuthSyncMU = sync.Mutex{}

// VMAuthReconciler reconciles a VMAuth object
type VMAuthReconciler struct {
	client.Client
	BaseConf     *config.BaseOperatorConf
	Log          logr.Logger
	OriginScheme *runtime.Scheme
}

// Scheme implements interface.
func (r *VMAuthReconciler) Scheme() *runtime.Scheme {
	return r.OriginScheme
}

// Reconcile implements interface
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmauths,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmauths/status,verbs=get;update;patch
func (r *VMAuthReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = context.Background()
	l := r.Log.WithValues("vmauth", req.NamespacedName)

	var instance operatorv1beta1.VMAuth

	err := r.Get(ctx, req.NamespacedName, &instance)
	if err != nil {
		return handleGetError(req, "vmauth", err)
	}

	if !instance.DeletionTimestamp.IsZero() {
		if err := finalize.OnVMAuthDelete(ctx, r, &instance); err != nil {
			l.Error(err, "cannot remove finalizers from vmauth")
			return ctrl.Result{}, err
		}
		DeregisterObject(instance.Name, instance.Namespace, "vmauth")
		return ctrl.Result{}, nil
	}

	RegisterObject(instance.Name, instance.Namespace, "vmauth")

	if err := finalize.AddFinalizer(ctx, r.Client, &instance); err != nil {
		return ctrl.Result{}, err
	}

	if err := factory.CreateOrUpdateVMAuth(ctx, &instance, r, r.BaseConf); err != nil {
		l.Error(err, "cannot create or update vmauth deploy")
		return ctrl.Result{}, err
	}

	svc, err := factory.CreateOrUpdateVMAuthService(ctx, &instance, r)
	if err != nil {
		l.Error(err, "cannot create or update vmauth service")
		return ctrl.Result{}, err
	}
	if err := factory.CreateOrUpdateVMAuthIngress(ctx, r, &instance); err != nil {
		l.Error(err, "cannot createOrUpdateIngress for VMAuth")
		return ctrl.Result{}, err
	}

	if !r.BaseConf.DisableSelfServiceScrapeCreation {
		err := factory.CreateVMServiceScrapeFromService(ctx, r, svc, instance.Spec.ServiceScrapeSpec, instance.MetricPath())
		if err != nil {
			l.Error(err, "cannot create serviceScrape for vmauth")
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager inits object.
func (r *VMAuthReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorv1beta1.VMAuth{}).
		Owns(&v1.Secret{}, builder.OnlyMetadata).
		Owns(&appsv1.Deployment{}, builder.OnlyMetadata).
		Owns(&v1.Service{}, builder.OnlyMetadata).
		Owns(&v1.ServiceAccount{}, builder.OnlyMetadata).
		WithOptions(defaultOptions).
		Complete(r)
}
