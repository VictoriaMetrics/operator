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
	"github.com/VictoriaMetrics/operator/controllers/factory/k8stools"
	"github.com/VictoriaMetrics/operator/controllers/factory/logger"
	"github.com/VictoriaMetrics/operator/controllers/factory/vmagent"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// VMPodScrapeReconciler reconciles a VMPodScrape object
type VMPodScrapeReconciler struct {
	client.Client
	Log          logr.Logger
	OriginScheme *runtime.Scheme
	BaseConf     *config.BaseOperatorConf
}

// Scheme implements interface.
func (r *VMPodScrapeReconciler) Scheme() *runtime.Scheme {
	return r.OriginScheme
}

// Reconcile general reconcile method for controller
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmpodscrapes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmpodscrapes/status,verbs=get;update;patch
func (r *VMPodScrapeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	reqLogger := r.Log.WithValues("vmpodscrape", req.NamespacedName)
	ctx = logger.AddToContext(ctx, reqLogger)

	defer func() {
		result, err = handleReconcileErr(ctx, r.Client, nil, result, err)
	}()
	// Fetch the VMPodScrape instance
	instance := &victoriametricsv1beta1.VMPodScrape{}
	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		return result, &getError{err, "vmpodscrape", req}
	}

	RegisterObjectStat(instance, "vmpodscrape")

	if vmAgentReconcileLimit.MustThrottleReconcile() {
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

	for _, vmagentItem := range objects.Items {
		if !vmagentItem.DeletionTimestamp.IsZero() || vmagentItem.Spec.ParsingError != "" || vmagentItem.IsUnmanaged() {
			continue
		}
		reqLogger = reqLogger.WithValues("vmagent", vmagentItem.Name)
		currentVMagent := &vmagentItem
		if !currentVMagent.Spec.SelectAllByDefault {
			match, err := isSelectorsMatchesTargetCRD(ctx, r.Client, instance, currentVMagent, currentVMagent.Spec.PodScrapeSelector, currentVMagent.Spec.PodScrapeNamespaceSelector)
			if err != nil {
				reqLogger.Error(err, "cannot match vmagent and vmPodScrape")
				continue
			}
			if !match {
				continue
			}
		}
		reqLogger := reqLogger.WithValues("vmagent", currentVMagent.Name)
		ctx := logger.AddToContext(ctx, reqLogger)

		if err := vmagent.CreateOrUpdateConfigurationSecret(ctx, currentVMagent, r, r.BaseConf); err != nil {
			continue
		}
	}

	return
}

// SetupWithManager general setup method
func (r *VMPodScrapeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&victoriametricsv1beta1.VMPodScrape{}).
		WithOptions(getDefaultOptions()).
		Complete(r)
}
