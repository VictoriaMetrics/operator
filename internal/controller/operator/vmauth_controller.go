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
	"fmt"

	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/vmauth"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

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
func (r *VMAuthReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	l := r.Log.WithValues("vmauth", req.Name, "namespace", req.Namespace)
	ctx = logger.AddToContext(ctx, l)
	instance := &vmv1beta1.VMAuth{}

	defer func() {
		result, err = handleReconcileErr(ctx, r.Client, instance, result, err)
	}()

	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		return result, &getError{err, "vmauth", req}
	}

	RegisterObjectStat(instance, "vmauth")

	if !instance.DeletionTimestamp.IsZero() {
		if err := finalize.OnVMAuthDelete(ctx, r, instance); err != nil {
			return result, fmt.Errorf("cannot remove finalizer from vmauth: %w", err)
		}
		return result, nil
	}
	if instance.Spec.ParsingError != "" {
		return result, &parsingError{instance.Spec.ParsingError, "vmauth"}
	}

	if err := finalize.AddFinalizer(ctx, r.Client, instance); err != nil {
		return result, err
	}
	r.Client.Scheme().Default(instance)

	result, err = reconcileAndTrackStatus(ctx, r.Client, instance, func() (ctrl.Result, error) {
		if err := vmauth.CreateOrUpdateVMAuth(ctx, instance, r); err != nil {
			return result, fmt.Errorf("cannot create or update vmauth deploy: %w", err)
		}

		svc, err := vmauth.CreateOrUpdateVMAuthService(ctx, instance, r)
		if err != nil {
			return result, fmt.Errorf("cannot create or update vmauth service :%w", err)
		}
		if err := vmauth.CreateOrUpdateVMAuthIngress(ctx, r, instance); err != nil {
			return result, fmt.Errorf("cannot create or update ingress for vmauth: %w", err)
		}
		// TODO delete conditionally
		if !ptr.Deref(instance.Spec.DisableSelfServiceScrape, false) {
			if err := reconcile.VMServiceScrapeForCRD(ctx, r, build.VMServiceScrapeForServiceWithSpec(svc, instance)); err != nil {
				l.Error(err, "cannot create serviceScrape for vmauth")
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

// SetupWithManager inits object.
func (r *VMAuthReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&vmv1beta1.VMAuth{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.ServiceAccount{}).
		WithOptions(getDefaultOptions()).
		Complete(r)
}
