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
	"fmt"

	"github.com/VictoriaMetrics/operator/controllers/factory"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/go-logr/logr"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/controllers/factory/finalize"
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
func (r *VMSingleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	reqLogger := r.Log.WithValues("vmsingle", req.NamespacedName)

	instance := &victoriametricsv1beta1.VMSingle{}
	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		return handleGetError(req, "vmsingle", err)
	}

	RegisterObjectStat(instance, "vmsingle")
	if !instance.DeletionTimestamp.IsZero() {
		if err := finalize.OnVMSingleDelete(ctx, r.Client, instance); err != nil {
			return result, err
		}
		return
	}
	if instance.Spec.ParsingError != "" {
		return handleParsingError(instance.Spec.ParsingError, instance)
	}
	specChanged, err := instance.HasSpecChanges()
	if err != nil {
		reqLogger.Error(err, "failed to check if single spec changed")
	}
	if specChanged && instance.Status.SingleStatus != victoriametricsv1beta1.SingleStatusFailed {
		instance.Status.SingleStatus = victoriametricsv1beta1.SingleStatusExpanding
		if err := r.Client.Status().Update(ctx, instance); err != nil {
			return result, fmt.Errorf("cannot set expanding status for single: %w", err)
		}
	}

	if err := finalize.AddFinalizer(ctx, r.Client, instance); err != nil {
		return result, err
	}

	if instance.Spec.Storage != nil && instance.Spec.StorageDataPath == "" {
		_, err = factory.CreateVMSingleStorage(ctx, instance, r)
		if err != nil {
			return result, err
		}
	}

	if err := factory.CreateOrUpdateVMSingleStreamAggrConfig(ctx, instance, r); err != nil {
		return result, fmt.Errorf("cannot update stream aggregation config for vmsingle: %w", err)
	}

	_, err = factory.CreateOrUpdateVMSingle(ctx, instance, r, r.BaseConf)
	if err != nil {
		instance.Status.Reason = err.Error()
		instance.Status.SingleStatus = victoriametricsv1beta1.SingleStatusFailed
		if err := r.Client.Status().Update(ctx, instance); err != nil {
			log.Error(err, "cannot set failed status for single")
		}
		return result, fmt.Errorf("failed create or update single: %w", err)
	}

	svc, err := factory.CreateOrUpdateVMSingleService(ctx, instance, r, r.BaseConf)
	if err != nil {
		return result, err
	}

	if !r.BaseConf.DisableSelfServiceScrapeCreation {
		err := factory.CreateVMServiceScrapeFromService(ctx, r, svc, instance.Spec.ServiceScrapeSpec, instance.MetricPath())
		if err != nil {
			reqLogger.Error(err, "cannot create serviceScrape for vmsingle")
		}
	}

	instance.Status.Reason = ""
	instance.Status.SingleStatus = victoriametricsv1beta1.SingleStatusOperational
	if err := r.Client.Status().Update(ctx, instance); err != nil {
		return result, fmt.Errorf("cannot update single status: %w", err)
	}

	if specChanged {
		specPatch, err := instance.LastAppliedSpecAsPatch()
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("cannot parse last applied spec for single: %w", err)
		}
		// use patch instead of update, only 1 field must be changed.
		if err := r.Client.Patch(ctx, instance, specPatch); err != nil {
			return result, fmt.Errorf("cannot update single with last applied spec: %w", err)
		}
	}

	return
}

// SetupWithManager general setup method
func (r *VMSingleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&victoriametricsv1beta1.VMSingle{}).
		Owns(&appsv1.Deployment{}, builder.OnlyMetadata).
		Owns(&v1.Service{}, builder.OnlyMetadata).
		Owns(&victoriametricsv1beta1.VMServiceScrape{}).
		Owns(&v1.ServiceAccount{}, builder.OnlyMetadata).
		WithOptions(getDefaultOptions()).
		Complete(r)
}
