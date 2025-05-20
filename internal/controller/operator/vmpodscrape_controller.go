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
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/vmagent"
)

// VMPodScrapeReconciler reconciles a VMPodScrape object
type VMPodScrapeReconciler struct {
	client.Client
	Log          logr.Logger
	OriginScheme *runtime.Scheme
}

// Init implements crdController interface
func (r *VMPodScrapeReconciler) Init(rclient client.Client, l logr.Logger, sc *runtime.Scheme, cf *config.BaseOperatorConf) {
	r.Client = rclient
	r.Log = l.WithName("controller.VMPodScrape")
	r.OriginScheme = sc
}

// Scheme implements interface.
func (r *VMPodScrapeReconciler) Scheme() *runtime.Scheme {
	return r.OriginScheme
}

// Reconcile general reconcile method for controller
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmpodscrapes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmpodscrapes/status,verbs=get;update;patch
func (r *VMPodScrapeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	instance := &vmv1beta1.VMPodScrape{}
	reqLogger := r.Log.WithValues("vmpodscrape", req.Name, "namespace", req.Namespace)
	ctx = logger.AddToContext(ctx, reqLogger)

	defer func() {
		result, err = handleReconcileErrWithoutStatus(ctx, r.Client, instance, result, err)
	}()
	// Fetch the VMPodScrape instance
	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		return result, &getError{err, "vmpodscrape", req}
	}

	RegisterObjectStat(instance, "vmpodscrape")
	if instance.Spec.ParsingError != "" {
		return result, &parsingError{instance.Spec.ParsingError, "vmpodscrape"}
	}
	if vmAgentReconcileLimit.MustThrottleReconcile() {
		return
	}

	vmAgentSync.Lock()
	defer vmAgentSync.Unlock()

	var objects vmv1beta1.VMAgentList
	if err := k8stools.ListObjectsByNamespace(ctx, r.Client, config.MustGetWatchNamespaces(), func(dst *vmv1beta1.VMAgentList) {
		objects.Items = append(objects.Items, dst.Items...)
	}); err != nil {
		return result, fmt.Errorf("cannot list vmauths for vmuser: %w", err)
	}

	for _, vmagentItem := range objects.Items {
		if !vmagentItem.DeletionTimestamp.IsZero() || vmagentItem.Spec.ParsingError != "" || vmagentItem.IsPodScrapeUnmanaged() {
			continue
		}
		currentVMagent := &vmagentItem
		reqLogger := reqLogger.WithValues("vmagent", currentVMagent.Name, "parent_namespace", currentVMagent.Namespace)
		ctx := logger.AddToContext(ctx, reqLogger)

		// only check selector when deleting object,
		// since labels can be changed when updating and we can't tell if it was selected before, and we can't tell if it's creating or updating.
		if !instance.DeletionTimestamp.IsZero() {
			match, err := isSelectorsMatchesTargetCRD(ctx, r.Client, instance, currentVMagent, currentVMagent.Spec.PodScrapeSelector, currentVMagent.Spec.PodScrapeNamespaceSelector, currentVMagent.Spec.SelectAllByDefault)
			if err != nil {
				reqLogger.Error(err, "cannot match vmagent and vmPodScrape")
				continue
			}
			if !match {
				continue
			}
		}

		if err := vmagent.CreateOrUpdateConfigurationSecret(ctx, r, currentVMagent, instance); err != nil {
			continue
		}
	}

	return
}

// SetupWithManager general setup method
func (r *VMPodScrapeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&vmv1beta1.VMPodScrape{}).
		WithEventFilter(predicate.TypedGenerationChangedPredicate[client.Object]{}).
		WithOptions(getDefaultOptions()).
		Complete(r)
}
