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

package controller

import (
	"context"
	"sync"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/factory/limiter"
	"github.com/VictoriaMetrics/operator/internal/controller/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/controller/factory/reconcile"
	"github.com/VictoriaMetrics/operator/internal/controller/factory/vmalert"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	vmAlertRateLimiter = limiter.NewRateLimiter("vmalert", 5)
	vmAlertSync        sync.Mutex
)

// VMAlertReconciler reconciles a VMAlert object
type VMAlertReconciler struct {
	client.Client
	Log          logr.Logger
	OriginScheme *runtime.Scheme
	BaseConf     *config.BaseOperatorConf
}

// Scheme implements interface.
func (r *VMAlertReconciler) Scheme() *runtime.Scheme {
	return r.OriginScheme
}

// Reconcile general reconile method for controller
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmalerts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmalerts/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmalerts/finalizers,verbs=*
func (r *VMAlertReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, resultErr error) {
	reqLogger := r.Log.WithValues("vmalert", req.NamespacedName)
	ctx = logger.AddToContext(ctx, reqLogger)
	instance := &vmv1beta1.VMAlert{}

	defer func() {
		result, resultErr = handleReconcileErr(ctx, r.Client, instance, result, resultErr)
	}()

	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		return result, &getError{err, "vmalert", req}
	}

	RegisterObjectStat(instance, "vmalert")
	if !instance.IsUnmanaged() {
		vmAlertSync.Lock()
		defer vmAlertSync.Unlock()
	}
	if !instance.DeletionTimestamp.IsZero() {
		if err := finalize.OnVMAlertDelete(ctx, r.Client, instance); err != nil {
			return result, err
		}
		return result, nil
	}

	if instance.Spec.ParsingError != "" {
		return result, &parsingError{instance.Spec.ParsingError, "vmalert"}
	}

	var needToRequeue bool
	if len(instance.GetNotifierSelectors()) > 0 {
		needToRequeue = true
	}

	if err := finalize.AddFinalizer(ctx, r.Client, instance); err != nil {
		return result, err
	}

	result, resultErr = reconcileAndTrackStatus(ctx, r.Client, instance, func() (ctrl.Result, error) {
		maps, err := vmalert.CreateOrUpdateRuleConfigMaps(ctx, instance, r)
		if err != nil {
			return result, err
		}
		reqLogger.Info("found configmaps for vmalert", " len ", len(maps), "map names", maps)

		if err := vmalert.CreateOrUpdateVMAlert(ctx, instance, r, r.BaseConf, maps); err != nil {
			return result, err
		}

		svc, err := vmalert.CreateOrUpdateVMAlertService(ctx, instance, r, r.BaseConf)
		if err != nil {
			return result, err
		}

		if !r.BaseConf.DisableSelfServiceScrapeCreation {
			err := reconcile.VMServiceScrapeForCRD(ctx, r, build.VMServiceScrapeForServiceWithSpec(svc, instance.Spec.ServiceScrapeSpec, instance.MetricPath()))
			if err != nil {
				reqLogger.Error(err, "cannot create serviceScrape for vmalert")
			}
		}

		return result, nil
	})
	if resultErr != nil {
		return
	}

	if needToRequeue || r.BaseConf.ForceResyncInterval > 0 {
		result.RequeueAfter = r.BaseConf.ForceResyncInterval
	}

	return result, nil
}

// SetupWithManager general setup method
func (r *VMAlertReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&vmv1beta1.VMAlert{}).
		Owns(&appsv1.Deployment{}).
		Owns(&v1.ServiceAccount{}).
		WithOptions(getDefaultOptions()).
		Complete(r)
}
