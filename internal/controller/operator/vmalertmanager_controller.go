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

package operator

import (
	"context"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/alertmanager"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// VMAlertmanagerReconciler reconciles a VMAlertmanager object
type VMAlertmanagerReconciler struct {
	client.Client
	Log          logr.Logger
	OriginScheme *runtime.Scheme
	BaseConf     *config.BaseOperatorConf
}

// Scheme implements interface.
func (r *VMAlertmanagerReconciler) Scheme() *runtime.Scheme {
	return r.OriginScheme
}

// Reconcile general reconcile method for controller
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmalertmanagers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmalertmanagers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmalertmanagers/finalizers,verbs=*
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=*
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=*
// +kubebuilder:rbac:groups="",resources=secrets,verbs=*
func (r *VMAlertmanagerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	reqLogger := r.Log.WithValues("vmalertmanager", req.NamespacedName)
	ctx = logger.AddToContext(ctx, reqLogger)
	instance := &vmv1beta1.VMAlertmanager{}

	defer func() {
		result, err = handleReconcileErr(ctx, r.Client, instance, result, err)
	}()
	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		return result, &getError{err, "vmalertmanager", req}
	}
	RegisterObjectStat(instance, "vmalertmanager")

	if !instance.DeletionTimestamp.IsZero() {
		if err := finalize.OnVMAlertManagerDelete(ctx, r.Client, instance); err != nil {
			return result, err
		}
		return
	}
	if instance.Spec.ParsingError != "" {
		return result, &parsingError{instance.Spec.ParsingError, "vmalertmanager"}
	}

	if err := finalize.AddFinalizer(ctx, r.Client, instance); err != nil {
		return result, err
	}
	r.Client.Scheme().Default(instance)

	result, err = reconcileAndTrackStatus(ctx, r.Client, instance, func() (ctrl.Result, error) {
		if err := alertmanager.CreateAMConfig(ctx, instance, r.Client); err != nil {
			return result, err
		}

		if err := alertmanager.CreateOrUpdateAlertManager(ctx, instance, r); err != nil {
			return result, err
		}

		service, err := alertmanager.CreateOrUpdateAlertManagerService(ctx, instance, r)
		if err != nil {
			return result, err
		}

		// TODO delete conditionally
		if !ptr.Deref(instance.Spec.DisableSelfServiceScrape, false) {
			err := reconcile.VMServiceScrapeForCRD(ctx, r, build.VMServiceScrapeForAlertmanager(service, instance))
			if err != nil {
				reqLogger.Error(err, "cannot create serviceScrape for vmalertmanager")
			}
		}
		return result, nil
	})
	if err != nil {
		return
	}

	result.RequeueAfter = r.BaseConf.ResyncAfterDuration()
	return
}

// SetupWithManager general setup method
func (r *VMAlertmanagerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&vmv1beta1.VMAlertmanager{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&v1.ServiceAccount{}).
		WithOptions(getDefaultOptions()).
		Complete(r)
}
