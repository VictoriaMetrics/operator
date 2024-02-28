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

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
)

var alertmanagerLock sync.Mutex

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

	instance := &victoriametricsv1beta1.VMAlertmanager{}
	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		return handleGetError(req, "vmalertmanager", err)
	}
	RegisterObjectStat(instance, "vmalertmanager")

	if !instance.DeletionTimestamp.IsZero() {
		if err := finalize.OnVMAlertManagerDelete(ctx, r.Client, instance); err != nil {
			return result, err
		}
		return
	}
	if instance.Spec.ParsingError != "" {
		return handleParsingError(instance.Spec.ParsingError, instance)
	}
	// add an optimisation for unmmanaged alertmanagers
	if instance.IsUnmanaged() {
		alertmanagerLock.Lock()
		defer alertmanagerLock.Unlock()
	}

	if err := finalize.AddFinalizer(ctx, r.Client, instance); err != nil {
		return result, err
	}
	result, err = reconcileAndTrackStatus(ctx, r.Client, instance, func() (ctrl.Result, error) {
		if err := factory.CreateOrUpdateAlertManager(ctx, instance, r, r.BaseConf); err != nil {
			return result, err
		}
		service, err := factory.CreateOrUpdateAlertManagerService(ctx, instance, r)
		if err != nil {
			return result, err
		}

		if !r.BaseConf.DisableSelfServiceScrapeCreation {
			err := factory.CreateVMServiceScrapeFromService(ctx, r, service, instance.Spec.ServiceScrapeSpec, instance.MetricPath(), "http")
			if err != nil {
				reqLogger.Error(err, "cannot create serviceScrape for vmalertmanager")
			}
		}
		return result, nil
	})
	if err != nil {
		return
	}

	// resync configuration periodically
	if r.BaseConf.ForceResyncInterval > 0 {
		result.RequeueAfter = r.BaseConf.ForceResyncInterval
	}
	return
}

// SetupWithManager general setup method
func (r *VMAlertmanagerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&victoriametricsv1beta1.VMAlertmanager{}).
		Owns(&appsv1.StatefulSet{}, builder.OnlyMetadata).
		Owns(&v1.Service{}, builder.OnlyMetadata).
		Owns(&victoriametricsv1beta1.VMServiceScrape{}, builder.OnlyMetadata).
		Owns(&v1.Secret{}, builder.OnlyMetadata).
		Owns(&v1.ServiceAccount{}, builder.OnlyMetadata).
		WithOptions(getDefaultOptions()).
		Complete(r)
}
