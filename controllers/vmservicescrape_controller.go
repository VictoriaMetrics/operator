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

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/controllers/factory"
	"github.com/VictoriaMetrics/operator/controllers/factory/k8stools"
	"github.com/VictoriaMetrics/operator/controllers/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// VMServiceScrapeReconciler reconciles a VMServiceScrape object
type VMServiceScrapeReconciler struct {
	client.Client
	Log          logr.Logger
	OriginScheme *runtime.Scheme
	BaseConf     *config.BaseOperatorConf
}

// Scheme implements interface.
func (r *VMServiceScrapeReconciler) Scheme() *runtime.Scheme {
	return r.OriginScheme
}

// Reconcile general reconcile method for controller
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmservicescrapes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmservicescrapes/status,verbs=get;update;patch
func (r *VMServiceScrapeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	reqLogger := r.Log.WithValues("vmservicescrape", req.NamespacedName)
	// Fetch the VMServiceScrape instance
	instance := &victoriametricsv1beta1.VMServiceScrape{}
	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		return handleGetError(req, "vmservicescrape", err)
	}

	RegisterObjectStat(instance, "vmservicescrape")
	if vmAgentReconcileLimit.MustThrottleReconcile() {
		// fast path, rate limited
		return
	}

	vmAgentSync.Lock()
	defer vmAgentSync.Unlock()
	var objects victoriametricsv1beta1.VMAgentList
	if err := k8stools.ListObjectsByNamespace(ctx, r.Client, config.MustGetWatchNamespaces(), func(dst *victoriametricsv1beta1.VMAgentList) {
		objects.Items = append(objects.Items, dst.Items...)
	}); err != nil {
		return result, fmt.Errorf("cannot list vmauths for vmuser: %w", err)
	}

	for _, vmagent := range objects.Items {
		if !vmagent.DeletionTimestamp.IsZero() || vmagent.Spec.ParsingError != "" || vmagent.IsUnmanaged() {
			continue
		}
		currentVMagent := &vmagent
		if !currentVMagent.Spec.SelectAllByDefault {
			match, err := isSelectorsMatches(r.Client, instance, currentVMagent, currentVMagent.Spec.ServiceScrapeSelector, currentVMagent.Spec.ServiceScrapeNamespaceSelector)
			if err != nil {
				reqLogger.Error(err, "cannot match vmagent and vmServiceScrape")
				continue
			}
			if !match {
				continue
			}
		}
		reqLogger := reqLogger.WithValues("vmagent", currentVMagent.Name)
		ctx := logger.AddToContext(ctx, reqLogger)

		if err := factory.CreateOrUpdateVMAgent(ctx, currentVMagent, r, r.BaseConf); err != nil {
			reqLogger.Error(err, "cannot create or update vmagent instance")
			continue
		}
	}
	return
}

// SetupWithManager general setup method
func (r *VMServiceScrapeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&victoriametricsv1beta1.VMServiceScrape{}).
		WithOptions(getDefaultOptions()).
		Complete(r)
}
