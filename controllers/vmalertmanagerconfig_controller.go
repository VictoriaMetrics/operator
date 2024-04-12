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

	"github.com/VictoriaMetrics/operator/controllers/factory/k8stools"
	"github.com/VictoriaMetrics/operator/controllers/factory/limiter"
	"github.com/VictoriaMetrics/operator/controllers/factory/logger"

	"github.com/VictoriaMetrics/operator/controllers/factory"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
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
func (r *VMAlertmanagerConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	l := r.Log.WithValues("vmalertmanagerconfig", req.NamespacedName, "name", req.Name)

	var instance operatorv1beta1.VMAlertmanagerConfig
	if err := r.Client.Get(ctx, req.NamespacedName, &instance); err != nil {
		return handleGetError(req, "vmalertmanagerconfig", err)
	}

	RegisterObjectStat(&instance, "vmalertmanagerconfig")

	if vmaConfigRateLimiter.MustThrottleReconcile() {
		return
	}

	alertmanagerLock.Lock()
	defer alertmanagerLock.Unlock()

	var objects victoriametricsv1beta1.VMAlertmanagerList
	if err := k8stools.ListObjectsByNamespace(ctx, r.Client, config.MustGetWatchNamespaces(), func(dst *victoriametricsv1beta1.VMAlertmanagerList) {
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

		if !am.Spec.SelectAllByDefault {
			match, err := isSelectorsMatches(r.Client, &instance, am, am.Spec.ConfigSelector, am.Spec.ConfigNamespaceSelector)
			if err != nil {
				l.Error(err, "cannot match alertmanager against selector, probably bug")
				continue
			}
			if !match {
				continue
			}
		}
		if err := factory.CreateOrUpdateAlertManager(ctx, am, r.Client, r.BaseConf); err != nil {
			l.Error(err, "cannot  reconcile alertmanager")
			continue
		}
	}
	return
}

// SetupWithManager configures reconcile
func (r *VMAlertmanagerConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorv1beta1.VMAlertmanagerConfig{}).
		WithOptions(getDefaultOptions()).
		Complete(r)
}
