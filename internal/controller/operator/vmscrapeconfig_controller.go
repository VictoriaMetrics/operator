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

// VMScrapeConfigReconciler reconciles a VMScrapeConfig object
type VMScrapeConfigReconciler struct {
	client.Client
	Log          logr.Logger
	OriginScheme *runtime.Scheme
}

// Init implements crdController interface
func (r *VMScrapeConfigReconciler) Init(rclient client.Client, l logr.Logger, sc *runtime.Scheme, cf *config.BaseOperatorConf) {
	r.Client = rclient
	r.Log = l.WithName("controller.VMScrapeConfig")
	r.OriginScheme = sc
}

// Scheme implements interface.
func (r *VMScrapeConfigReconciler) Scheme() *runtime.Scheme {
	return r.OriginScheme
}

// Reconcile general reconcile method for controller
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmscrapeconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmscrapeconfigs/status,verbs=get;update;patch
func (r *VMScrapeConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	instance := &vmv1beta1.VMScrapeConfig{}
	l := r.Log.WithValues("vmscrapeconfig", req.Name, "namespace", req.Namespace)
	ctx = logger.AddToContext(ctx, l)
	defer func() {
		result, err = handleReconcileErrWithoutStatus(ctx, r.Client, instance, result, err)
	}()

	// Fetch the VMScrapeConfig instance
	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		return result, &getError{err, "vmscrapeconfig", req}
	}

	RegisterObjectStat(instance, "vmscrapeconfig")
	if instance.Spec.ParsingError != "" {
		return result, &parsingError{instance.Spec.ParsingError, "vmscrapeconfig"}
	}
	if agentReconcileLimit.MustThrottleReconcile() {
		// fast path, rate limited
		return
	}

	agentSync.Lock()
	defer agentSync.Unlock()
	var objects vmv1beta1.VMAgentList
	if err := k8stools.ListObjectsByNamespace(ctx, r.Client, config.MustGetWatchNamespaces(), func(dst *vmv1beta1.VMAgentList) {
		objects.Items = append(objects.Items, dst.Items...)
	}); err != nil {
		return result, fmt.Errorf("cannot list vmagents for vmscrapeconfig: %w", err)
	}

	for i := range objects.Items {
		item := &objects.Items[i]
		if !item.DeletionTimestamp.IsZero() || item.Spec.ParsingError != "" || item.IsScrapeConfigUnmanaged() {
			continue
		}
		l := l.WithValues("vmagent", item.Name, "parent_namespace", item.Namespace)
		ctx := logger.AddToContext(ctx, l)
		if item.Spec.DaemonSetMode {
			continue
		}
		// only check selector when deleting object,
		// since labels can be changed when updating and we can't tell if it was selected before, and we can't tell if it's creating or updating.
		if !instance.DeletionTimestamp.IsZero() {
			opts := &k8stools.SelectorOpts{
				SelectAll:         item.Spec.SelectAllByDefault,
				NamespaceSelector: item.Spec.ScrapeConfigNamespaceSelector,
				ObjectSelector:    item.Spec.ScrapeConfigSelector,
				DefaultNamespace:  instance.Namespace,
			}
			match, err := isSelectorsMatchesTargetCRD(ctx, r.Client, instance, item, opts)
			if err != nil {
				l.Error(err, "cannot match vmagent and vmscrapeconfig")
				continue
			}
			if !match {
				continue
			}
		}

		if err := vmagent.CreateOrUpdateConfigurationSecret(ctx, r, item, instance); err != nil {
			continue
		}
	}
	return
}

// SetupWithManager general setup method
func (r *VMScrapeConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&vmv1beta1.VMScrapeConfig{}).
		WithEventFilter(predicate.TypedGenerationChangedPredicate[client.Object]{}).
		WithOptions(getDefaultOptions()).
		Complete(r)
}
