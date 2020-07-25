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
	"github.com/VictoriaMetrics/operator/conf"
	"github.com/VictoriaMetrics/operator/controllers/factory"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
)

// VMAlertReconciler reconciles a VMAlert object
type VMAlertReconciler struct {
	client.Client
	Log      logr.Logger
	Scheme   *runtime.Scheme
	BaseConf *conf.BaseOperatorConf
}

// Reconcile general reconile method for controller
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmalerts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmalerts/status,verbs=get;update;patch
func (r *VMAlertReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	reqLogger := r.Log.WithValues("vmalert", req.NamespacedName)
	reqLogger.Info("Reconciling")

	// Fetch the VMAlert instance
	ctx := context.Background()
	instance := &victoriametricsv1beta1.VMAlert{}
	err := r.Get(ctx, req.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
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
		reqLogger.Error(err, "cannot create or update update  vmalert service")
		return ctrl.Result{}, err
	}

	//create vmservicescrape for object by default
	if !r.BaseConf.DisableSelfServiceMonitorCreation {
		err := factory.CreateVMServiceScrapeFromService(ctx, r, svc)
		if err != nil {
			reqLogger.Error(err, "cannot create serviceScrape for vmalert")
		}
	}

	reqLogger.Info("vmalert reconciled")

	return ctrl.Result{}, nil
}

// SetupWithManager general setup method
func (r *VMAlertReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&victoriametricsv1beta1.VMAlert{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)
}
