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
	"github.com/VictoriaMetrics/operator/controllers/factory/limiter"
	"sync"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/controllers/factory"
	"github.com/VictoriaMetrics/operator/controllers/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	policyv1beta1 "k8s.io/api/policy/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
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
func (r *VMAlertReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if vmAlertRateLimiter.MustThrottleReconcile() {
		// fast path
		return ctrl.Result{}, nil
	}
	reqLogger := r.Log.WithValues("vmalert", req.NamespacedName)
	reqLogger.Info("Reconciling")

	vmAlertSync.Lock()
	defer vmAlertSync.Unlock()

	// Fetch the VMAlert instance
	instance := &victoriametricsv1beta1.VMAlert{}
	err := r.Get(ctx, req.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if !instance.DeletionTimestamp.IsZero() {
		if err := finalize.OnVMAlertDelete(ctx, r.Client, instance); err != nil {
			return ctrl.Result{}, err
		}
		DeregisterObject(instance.Name, instance.Namespace, "vmalert")
		return ctrl.Result{}, nil
	}

	RegisterObject(instance.Name, instance.Namespace, "vmalert")

	var needToRequeue bool
	if len(instance.GetNotifierSelectors()) > 0 {
		needToRequeue = true
	}

	if err := finalize.AddFinalizer(ctx, r.Client, instance); err != nil {
		return ctrl.Result{}, err
	}

	maps, err := factory.CreateOrUpdateRuleConfigMaps(ctx, instance, r)
	if err != nil {
		reqLogger.Error(err, "cannot create or update vmalert cm")
		return ctrl.Result{}, err
	}
	reqLogger.Info("found configmaps for vmalert", " len ", len(maps), "map names", maps)

	recon, err := factory.CreateOrUpdateVMAlert(ctx, instance, r, r.BaseConf, maps)
	if err != nil {
		reqLogger.Error(err, "cannot create or update vmalert deploy")
		return recon, err
	}

	svc, err := factory.CreateOrUpdateVMAlertService(ctx, instance, r, r.BaseConf)
	if err != nil {
		reqLogger.Error(err, "cannot create or update vmalert service")
		return ctrl.Result{}, err
	}

	if !r.BaseConf.DisableSelfServiceScrapeCreation {
		err := factory.CreateVMServiceScrapeFromService(ctx, r, svc, instance.Spec.ServiceScrapeSpec, instance.MetricPath())
		if err != nil {
			// made on best effort.
			reqLogger.Error(err, "cannot create serviceScrape for vmalert")
		}
	}

	reqLogger.Info("vmalert reconciled")
	var result ctrl.Result
	if needToRequeue || r.BaseConf.ForceResyncInterval > 0 {
		result.RequeueAfter = r.BaseConf.ForceResyncInterval
	}

	return result, nil
}

// SetupWithManager general setup method
func (r *VMAlertReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&victoriametricsv1beta1.VMAlert{}).
		Owns(&appsv1.Deployment{}, builder.OnlyMetadata).
		Owns(&victoriametricsv1beta1.VMServiceScrape{}, builder.OnlyMetadata).
		Owns(&v1.Service{}, builder.OnlyMetadata).
		Owns(&v1.ConfigMap{}, builder.OnlyMetadata).
		Owns(&v1.Secret{}, builder.OnlyMetadata).
		Owns(&v1.ServiceAccount{}, builder.OnlyMetadata).
		Owns(&policyv1beta1.PodDisruptionBudget{}).
		Complete(r)
}
