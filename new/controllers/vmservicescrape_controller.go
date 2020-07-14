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
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
)

// VMServiceScrapeReconciler reconciles a VMServiceScrape object
type VMServiceScrapeReconciler struct {
	client.Client
	Log      logr.Logger
	Scheme   *runtime.Scheme
	BaseConf *conf.BaseOperatorConf
}

// +kubebuilder:rbac:groups=victoriametrics.victoriametrics.com,resources=vmservicescrapes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=victoriametrics.victoriametrics.com,resources=vmservicescrapes/status,verbs=get;update;patch

func (r *VMServiceScrapeReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	reqLogger := r.Log.WithValues("vmservicescrape", req.NamespacedName)
	reqLogger.Info("Reconciling VMServiceScrape")
	// Fetch the VMServiceScrape instance
	instance := &victoriametricsv1beta1.VMServiceScrape{}
	ctx := context.Background()
	err := r.Get(ctx, req.NamespacedName, instance)
	if err != nil {
		//in case of object notfound we must update vmagents
		if !errors.IsNotFound(err) {
			// Error reading the object - requeue the request.
			reqLogger.Error(err, "cannot get service scrape")
			return ctrl.Result{}, err
		}
	}
	vmAgentInstances := &victoriametricsv1beta1.VMAgentList{}
	err = r.List(ctx, vmAgentInstances)
	if err != nil {
		reqLogger.Error(err, "cannot list vmagent objects")
		return ctrl.Result{}, err
	}
	reqLogger.Info("found vmagent objects ", "len: ", len(vmAgentInstances.Items))

	for _, vmagent := range vmAgentInstances.Items {
		reqLogger = reqLogger.WithValues("vmagent", vmagent.Name)
		reqLogger.Info("reconciling servicescrapes for vmagent")
		currentVMagent := &vmagent

		recon, err := factory.CreateOrUpdateVMAgent(ctx, currentVMagent, r, r.BaseConf)
		if err != nil {
			reqLogger.Error(err, "cannot create or update vmagent instance")
			return recon, err
		}
		reqLogger.Info("reconciled vmagent")
	}

	reqLogger.Info("reconciled serviceScrape")
	return ctrl.Result{}, nil
}

func (r *VMServiceScrapeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&victoriametricsv1beta1.VMServiceScrape{}).
		Complete(r)
}
