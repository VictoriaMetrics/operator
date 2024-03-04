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
	"sync"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/controllers/factory"
	"github.com/VictoriaMetrics/operator/controllers/factory/finalize"
	"github.com/VictoriaMetrics/operator/controllers/factory/limiter"
	"github.com/VictoriaMetrics/operator/controllers/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	vmAgentSync           sync.Mutex
	vmAgentReconcileLimit = limiter.NewRateLimiter("vmagent", 5)
)

// VMAgentReconciler reconciles a VMAgent object
type VMAgentReconciler struct {
	client.Client
	Log          logr.Logger
	OriginScheme *runtime.Scheme
	BaseConf     *config.BaseOperatorConf
}

// Reconcile general reconcile method
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmagents,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmagents/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmagents/finalizers,verbs=*
// +kubebuilder:rbac:groups="",resources=pods,verbs=*
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;watch;list
// +kubebuilder:rbac:groups="",resources=nodes/proxy,verbs=get;watch;list
// +kubebuilder:rbac:groups="networking.k8s.io",resources=ingresses,verbs=get;watch;list
// +kubebuilder:rbac:groups="",resources=events,verbs=*
// +kubebuilder:rbac:groups="",resources=endpoints,verbs=*
// +kubebuilder:rbac:groups="",resources=endpointslices,verbs=get;watch;list
// +kubebuilder:rbac:groups="",resources=services,verbs=*
// +kubebuilder:rbac:groups="",resources=services/finalizers,verbs=*
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=*,verbs=*
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;watch;list
// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=clusterrolebindings,verbs=get;create,update;list
// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=clusterroles,verbs=get;create,update;list
// +kubebuilder:rbac:groups="policy",resources=podsecuritypolicies,verbs=get;create,update;list
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;create,update;list
func (r *VMAgentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	reqLogger := r.Log.WithValues("vmagent", req.NamespacedName)
	ctx = logger.AddToContext(ctx, reqLogger)

	// Fetch the VMAgent instance
	instance := &victoriametricsv1beta1.VMAgent{}
	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		return handleGetError(req, "vmagent", err)
	}
	if !instance.IsUnmanaged() {
		vmAgentSync.Lock()
		defer vmAgentSync.Unlock()
	}

	RegisterObjectStat(instance, "vmagent")
	if !instance.DeletionTimestamp.IsZero() {
		if err := finalize.OnVMAgentDelete(ctx, r.Client, instance); err != nil {
			return result, err
		}
		return
	}

	if instance.Spec.ParsingError != "" {
		return handleParsingError(instance.Spec.ParsingError, instance)
	}

	if err := finalize.AddFinalizer(ctx, r.Client, instance); err != nil {
		return result, err
	}

	result, err = reconcileAndTrackStatus(ctx, r.Client, instance, func() (ctrl.Result, error) {
		if err := factory.CreateOrUpdateVMAgent(ctx, instance, r, r.BaseConf); err != nil {
			return result, err
		}

		svc, err := factory.CreateOrUpdateVMAgentService(ctx, instance, r, r.BaseConf)
		if err != nil {
			return result, err
		}

		if !r.BaseConf.DisableSelfServiceScrapeCreation {
			err := factory.CreateVMServiceScrapeFromService(ctx, r, svc, instance.Spec.ServiceScrapeSpec, instance.MetricPath(), "http")
			if err != nil {
				reqLogger.Error(err, "cannot create serviceScrape for vmagent")
			}
		}
		if err := updateVMAgentStatus(ctx, r.Client, instance); err != nil {
			return result, err
		}
		return result, nil
	})
	if r.BaseConf.ForceResyncInterval > 0 {
		result.RequeueAfter = r.BaseConf.ForceResyncInterval
	}

	return
}

func updateVMAgentStatus(ctx context.Context, c client.Client, instance *victoriametricsv1beta1.VMAgent) error {
	// default value
	replicaCount := int32(1)
	if instance.Spec.ReplicaCount != nil {
		replicaCount = *instance.Spec.ReplicaCount
	}
	instance.Status.Replicas = replicaCount
	var shardCnt int32
	if instance.Spec.ShardCount != nil {
		shardCnt = int32(*instance.Spec.ShardCount)
	}
	instance.Status.Shards = shardCnt
	if err := c.Status().Update(ctx, instance); err != nil {
		return fmt.Errorf("cannot update status for vmagent: %s: %w", instance.Name, err)
	}
	return nil
}

// Scheme implements interface.
func (r *VMAgentReconciler) Scheme() *runtime.Scheme {
	return r.OriginScheme
}

// SetupWithManager general setup method
func (r *VMAgentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&victoriametricsv1beta1.VMAgent{}).
		Owns(&appsv1.Deployment{}, builder.OnlyMetadata).
		Owns(&victoriametricsv1beta1.VMServiceScrape{}, builder.OnlyMetadata).
		Owns(&v1.ConfigMap{}, builder.OnlyMetadata).
		Owns(&v1.Service{}, builder.OnlyMetadata).
		Owns(&v1.Secret{}, builder.OnlyMetadata).
		Owns(&v1.ServiceAccount{}, builder.OnlyMetadata).
		WithOptions(getDefaultOptions()).
		Complete(r)
}
