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

	"github.com/VictoriaMetrics/operator/controllers/factory"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
)

// VMUserReconciler reconciles a VMUser object
type VMUserReconciler struct {
	client.Client
	BaseConf     *config.BaseOperatorConf
	Log          logr.Logger
	OriginScheme *runtime.Scheme
}

// Scheme implements interface.
func (r *VMUserReconciler) Scheme() *runtime.Scheme {
	return r.OriginScheme
}

// Reconcile implements interface
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmusers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmusers/status,verbs=get;update;patch
func (r *VMUserReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := r.Log.WithValues("vmuser", req.NamespacedName)

	var instance operatorv1beta1.VMUser

	err := r.Get(ctx, req.NamespacedName, &instance)
	if err != nil {
		//in case of object notfound we must update vmauthes
		if !errors.IsNotFound(err) {
			// Error reading the object - requeue the request.
			return ctrl.Result{}, err
		}
	}
	vmAuthSyncMU.Lock()
	defer vmAuthSyncMU.Unlock()
	var vmauthes operatorv1beta1.VMAuthList
	if err := r.List(ctx, &vmauthes); err != nil {
		l.Error(err, "cannot list VMAuth at cluster wide.")
		return ctrl.Result{}, err
	}
	for _, vmauth := range vmauthes.Items {
		// reconcile users for given vmauth.
		currentVMAuth := &vmauth
		l = l.WithValues("vmauth", vmauth.Name)
		match, err := isSelectorsMatches(&instance, currentVMAuth, currentVMAuth.Spec.UserNamespaceSelector, currentVMAuth.Spec.UserSelector)
		if err != nil {
			l.Error(err, "cannot match vmauth and VMUser")
			continue
		}
		// fast path
		if !match {
			continue
		}
		l.Info("reconciling vmuser for vmauth")
		if err := factory.CreateOrUpdateVMAuth(ctx, currentVMAuth, r, r.BaseConf); err != nil {
			l.Error(err, "cannot create or update vmauth deploy")
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

// SetupWithManager inits object
func (r *VMUserReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorv1beta1.VMUser{}).
		Complete(r)
}
