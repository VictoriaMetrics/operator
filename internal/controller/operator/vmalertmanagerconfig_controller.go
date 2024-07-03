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

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/alertmanager"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/limiter"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var vmaConfigRateLimiter = limiter.NewRateLimiter("vmalertmanager", 5)

// VMAlertmanagerConfigReconciler reconciles a VMAlertmanagerConfig object
type VMAlertmanagerConfigReconciler struct {
	client.Client
	Log          logr.Logger
	OriginScheme *runtime.Scheme
	BaseConf     *config.BaseOperatorConf
}

// Scheme implements interface.
func (r *VMAlertmanagerConfigReconciler) Scheme() *runtime.Scheme {
	return r.OriginScheme
}

// Reconcile implements interface
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmalertmanagerconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmalertmanagerconfigs/status,verbs=get;update;patch
func (r *VMAlertmanagerConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, resultErr error) {
	l := r.Log.WithValues("vmalertmanagerconfig", req.NamespacedName, "name", req.Name)
	defer func() {
		result, resultErr = handleReconcileErr(ctx, r.Client, nil, result, resultErr)
	}()

	var instance vmv1beta1.VMAlertmanagerConfig

	if err := r.Get(ctx, req.NamespacedName, &instance); err != nil {
		return result, &getError{err, "vmalertmanagerconfig", req}
	}

	RegisterObjectStat(&instance, "vmalertmanagerconfig")

	if vmaConfigRateLimiter.MustThrottleReconcile() {
		return
	}

	alertmanagerLock.Lock()
	defer alertmanagerLock.Unlock()

	var objects vmv1beta1.VMAlertmanagerList
	if err := k8stools.ListObjectsByNamespace(ctx, r.Client, config.MustGetWatchNamespaces(), func(dst *vmv1beta1.VMAlertmanagerList) {
		objects.Items = append(objects.Items, dst.Items...)
	}); err != nil {
		return result, fmt.Errorf("cannot list vmauths for vmuser: %w", err)
	}

	for _, item := range objects.Items {
		am := &item
		if !am.DeletionTimestamp.IsZero() || am.Spec.ParsingError != "" || am.IsUnmanaged() {
			continue
		}

		l := l.WithValues("alertmanager", am.Name)
		ctx := logger.AddToContext(ctx, l)

		// only check selector when deleting, since labels can be changed when updating and we can't tell if it was selected before.
		if instance.DeletionTimestamp.IsZero() && !am.Spec.SelectAllByDefault {
			match, err := isSelectorsMatchesTargetCRD(ctx, r.Client, &instance, am, am.Spec.ConfigSelector, am.Spec.ConfigNamespaceSelector)
			if err != nil {
				l.Error(err, "cannot match alertmanager against selector, probably bug")
				continue
			}
			if !match {
				continue
			}
		}
		if err := alertmanager.CreateOrUpdateAlertManager(ctx, am, r.Client, r.BaseConf); err != nil {
			continue
		}
	}
	return
}

// SetupWithManager configures reconcile
func (r *VMAlertmanagerConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&vmv1beta1.VMAlertmanagerConfig{}).
		WithOptions(getDefaultOptions()).
		Complete(r)
}
