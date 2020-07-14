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
	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
)

// VMAlertmanagerReconciler reconciles a VMAlertmanager object
type VMAlertmanagerReconciler struct {
	client.Client
	Log      logr.Logger
	Scheme   *runtime.Scheme
	BaseConf *conf.BaseOperatorConf
}

// +kubebuilder:rbac:groups=victoriametrics.victoriametrics.com,resources=vmalertmanagers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=victoriametrics.victoriametrics.com,resources=vmalertmanagers/status,verbs=get;update;patch

func (r *VMAlertmanagerReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	reqLogger := r.Log.WithValues("object", "vmalertmanager", req.NamespacedName)
	reqLogger.Info("Reconciling")
	ctx := context.Background()

	instance := &victoriametricsv1beta1.VMAlertmanager{}
	err := r.Get(ctx, req.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	_, err = factory.CreateOrUpdateAlertManager(ctx, instance, r, r.BaseConf)
	if err != nil {
		reqLogger.Error(err, "cannot create or update vmalertmanager sts")
		return ctrl.Result{}, err
	}
	_, err = factory.CreateOrUpdateAlertManagerService(ctx, instance, r, r.BaseConf)
	if err != nil {
		reqLogger.Error(err, "cannot create or update vmalertmanager service")
		return ctrl.Result{}, err
	}

	reqLogger.Info("vmalertmanager reconciled")
	return ctrl.Result{}, nil
}

func (r *VMAlertmanagerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&victoriametricsv1beta1.VMAlertmanager{}).
		Complete(r)
}
