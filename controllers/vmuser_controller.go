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

	operatorv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/controllers/factory"
	"github.com/VictoriaMetrics/operator/controllers/factory/finalize"
	"github.com/VictoriaMetrics/operator/controllers/factory/k8stools"
	"github.com/VictoriaMetrics/operator/controllers/factory/limiter"
	"github.com/VictoriaMetrics/operator/controllers/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/go-logr/logr"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

var vmauthRateLimiter = limiter.NewRateLimiter("vmauth", 5)

// Reconcile implements interface
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmusers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmusers/status,verbs=get;update;patch
func (r *VMUserReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	l := r.Log.WithValues("vmuser", req.NamespacedName)

	var instance operatorv1beta1.VMUser

	if err := r.Get(ctx, req.NamespacedName, &instance); err != nil {
		return handleGetError(req, "vmuser", err)
	}
	RegisterObjectStat(&instance, "vmuser")

	if !instance.DeletionTimestamp.IsZero() {
		// need to remove finalizer and delete related resources.
		if err := finalize.OnVMUserDelete(ctx, r, &instance); err != nil {
			return result, fmt.Errorf("cannot remove finalizer for vmuser: %w", err)
		}
	} else {
		if err := finalize.AddFinalizer(ctx, r.Client, &instance); err != nil {
			return result, err
		}
	}

	if vmauthRateLimiter.MustThrottleReconcile() {
		return
	}
	// lock vmauth sync.
	vmAuthSyncMU.Lock()
	defer vmAuthSyncMU.Unlock()

	var vmauthes operatorv1beta1.VMAuthList
	if err := k8stools.ListObjectsByNamespace(ctx, r.Client, config.MustGetWatchNamespaces(), func(dst *operatorv1beta1.VMAuthList) {
		vmauthes.Items = append(vmauthes.Items, dst.Items...)
	}); err != nil {
		return result, fmt.Errorf("cannot list vmauths for vmuser: %w", err)
	}

	for _, vmauth := range vmauthes.Items {
		if !vmauth.DeletionTimestamp.IsZero() || vmauth.Spec.ParsingError != "" || vmauth.IsUnmanaged() {
			continue
		}
		// reconcile users for given vmauth.
		currentVMAuth := &vmauth
		match, err := isSelectorsMatches(&instance, currentVMAuth, currentVMAuth.Spec.UserSelector)
		if err != nil {
			l.Error(err, "cannot match vmauth and VMUser")
			continue
		}
		// fast path
		if !match {
			continue
		}
		l = l.WithValues("vmauth", vmauth.Name)
		ctx := logger.AddToContext(ctx, l)

		if err := factory.CreateOrUpdateVMAuth(ctx, currentVMAuth, r, r.BaseConf); err != nil {
			return ctrl.Result{}, fmt.Errorf("cannot create or update vmauth deploy for vmuser: %w", err)
		}
	}
	return
}

// SetupWithManager inits object
func (r *VMUserReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorv1beta1.VMUser{}).
		Owns(&v1.Secret{}, builder.OnlyMetadata).
		WithOptions(getDefaultOptions()).
		Complete(r)
}
