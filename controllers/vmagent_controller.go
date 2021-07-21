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
	policyv1beta1 "k8s.io/api/policy/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
)

var vmAgentSync sync.Mutex

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
func (r *VMAgentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	reqLogger := r.Log.WithValues("vmagent", req.NamespacedName)
	reqLogger.Info("Reconciling")

	vmAgentSync.Lock()
	defer vmAgentSync.Unlock()
	// Fetch the VMAgent instance
	instance := &victoriametricsv1beta1.VMAgent{}
	err := r.Get(ctx, req.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		reqLogger.Error(err, "cannot get vmagent object for reconcile")
		return ctrl.Result{}, err
	}

	if !instance.DeletionTimestamp.IsZero() {
		if err := finalize.OnVMAgentDelete(ctx, r.Client, instance); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}
	if err := finalize.AddFinalizer(ctx, r.Client, instance); err != nil {
		return ctrl.Result{}, err
	}

	//create deploy
	reconResult, err := factory.CreateOrUpdateVMAgent(ctx, instance, r, r.BaseConf)
	if err != nil {
		reqLogger.Error(err, "cannot create or update vmagent deploy")
		return reconResult, err
	}

	//create service for monitoring
	svc, err := factory.CreateOrUpdateVMAgentService(ctx, instance, r, r.BaseConf)
	if err != nil {
		reqLogger.Error(err, "cannot create or update vmagent service")
		return ctrl.Result{}, err
	}

	//create vmservicescrape for object by default
	if !r.BaseConf.DisableSelfServiceScrapeCreation {
		err := factory.CreateVMServiceScrapeFromService(ctx, r, svc, instance.MetricPath())
		if err != nil {
			reqLogger.Error(err, "cannot create serviceScrape for vmagent")
		}
	}

	reqLogger.Info("reconciled vmagent")

	return ctrl.Result{}, nil
}

// Scheme implements interface.
func (r *VMAgentReconciler) Scheme() *runtime.Scheme {
	return r.OriginScheme
}

// SetupWithManager general setup method
func (r *VMAgentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&victoriametricsv1beta1.VMAgent{}).
		Owns(&appsv1.Deployment{}).
		Owns(&victoriametricsv1beta1.VMServiceScrape{}).
		Owns(&v1.ConfigMap{}).
		Owns(&v1.Service{}).
		//Owns(&v1.Secret{}).
		Owns(&v1.ServiceAccount{}).
		Owns(&policyv1beta1.PodDisruptionBudget{}).
		Complete(r)
}
