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

package operator

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/limiter"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/vmauth"
)

// VMUserReconciler reconciles a VMUser object
type VMUserReconciler struct {
	client.Client
	Log          logr.Logger
	OriginScheme *runtime.Scheme
}

// Init implements crdController interface
func (r *VMUserReconciler) Init(rclient client.Client, l logr.Logger, sc *runtime.Scheme, cf *config.BaseOperatorConf) {
	r.Client = rclient
	r.Log = l.WithName("controller.VMUser")
	r.OriginScheme = sc
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
	var instance vmv1beta1.VMUser
	l := r.Log.WithValues("vmuser", req.Name, "namespace", req.Namespace)
	defer func() {
		result, err = handleReconcileErrWithoutStatus(ctx, r.Client, &instance, result, err)
	}()

	if err := r.Get(ctx, req.NamespacedName, &instance); err != nil {
		return result, &getError{err, "vmuser", req}
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
	var vmauthes vmv1beta1.VMAuthList
	if err := k8stools.ListObjectsByNamespace(ctx, r.Client, config.MustGetWatchNamespaces(), func(dst *vmv1beta1.VMAuthList) {
		vmauthes.Items = append(vmauthes.Items, dst.Items...)
	}); err != nil {
		return result, fmt.Errorf("cannot list vmauths for vmuser: %w", err)
	}

	for _, vmauthItem := range vmauthes.Items {
		if !vmauthItem.DeletionTimestamp.IsZero() || vmauthItem.Spec.ParsingError != "" || vmauthItem.IsUnmanaged() {
			continue
		}
		// reconcile users for given vmauth.
		currentVMAuth := &vmauthItem
		l = l.WithValues("vmauth", currentVMAuth.Name, "parent_namespace", currentVMAuth.Namespace)
		ctx := logger.AddToContext(ctx, l)

		// only check selector when deleting object,
		// since labels can be changed when updating and we can't tell if it was selected before, and we can't tell if it's creating or updating.
		if !instance.DeletionTimestamp.IsZero() {
			match, err := isSelectorsMatchesTargetCRD(ctx, r.Client, &instance, currentVMAuth, currentVMAuth.Spec.UserSelector, currentVMAuth.Spec.UserNamespaceSelector, currentVMAuth.Spec.SelectAllByDefault)
			if err != nil {
				l.Error(err, "cannot match vmauth and VMUser")
				continue
			}
			if !match {
				continue
			}
		}
		if err := vmauth.CreateOrUpdateVMAuthConfig(ctx, r, currentVMAuth, &instance); err != nil {
			return ctrl.Result{}, fmt.Errorf("cannot create or update vmauth deploy for vmuser: %w", err)
		}
	}
	return
}

// SetupWithManager inits object
func (r *VMUserReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&vmv1beta1.VMUser{}).
		Owns(&corev1.Secret{}, builder.OnlyMetadata).
		WithEventFilter(predicate.TypedGenerationChangedPredicate[client.Object]{}).
		WithOptions(getDefaultOptions()).
		Complete(r)
}
