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
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/vmagent"
)

// VMNodeScrapeReconciler reconciles a VMNodeScrape object
type VMNodeScrapeReconciler struct {
	client.Client
	Log          logr.Logger
	OriginScheme *runtime.Scheme
	BaseConf     *config.BaseOperatorConf
}

// Init implements crdController interface
func (r *VMNodeScrapeReconciler) Init(rclient client.Client, l logr.Logger, sc *runtime.Scheme, cf *config.BaseOperatorConf) {
	r.Client = rclient
	r.Log = l.WithName("controller.VMNodeScrape")
	r.OriginScheme = sc
	r.BaseConf = cf
}

// Scheme implements interface.
func (r *VMNodeScrapeReconciler) Scheme() *runtime.Scheme {
	return r.OriginScheme
}

// Reconcile - reconciles VMNodeScrape objects.
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmnodescrapes,verbs=*
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmnodescrapes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmnodescrapes/finalizers,verbs=*
func (r *VMNodeScrapeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	l := r.Log.WithValues("vmnodescrape", req.Name, "namespace", req.Namespace)
	if build.IsControllerDisabled("VMAgent") {
		l.Info("skipping VMNodeScrape reconcile since VMAgent controller is disabled")
		return
	}
	instance := &vmv1beta1.VMNodeScrape{}
	ctx = logger.AddToContext(ctx, l)
	defer func() {
		result, err = handleReconcileErrWithoutStatus(ctx, r.Client, instance, result, err)
	}()

	// Fetch the VMNodeScrape instance
	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		return result, &getError{err, "vmnodescrape", req}
	}

	RegisterObjectStat(instance, "vmnodescrape")
	if instance.Spec.ParsingError != "" {
		return result, &parsingError{instance.Spec.ParsingError, "vmnodescrape"}
	}
	if agentReconcileLimit.MustThrottleReconcile() {
		// fast path, rate limited
		return
	}

	agentSync.Lock()
	defer agentSync.Unlock()

	var objects vmv1beta1.VMAgentList
	if err := k8stools.ListObjectsByNamespace(ctx, r.Client, r.BaseConf.WatchNamespaces, func(dst *vmv1beta1.VMAgentList) {
		objects.Items = append(objects.Items, dst.Items...)
	}); err != nil {
		return result, fmt.Errorf("cannot list vmagents for vmnodescrape: %w", err)
	}

	for i := range objects.Items {
		item := &objects.Items[i]
		if item.IsUnmanaged(instance) {
			continue
		}
		l := l.WithValues("vmagent", item.Name, "parent_namespace", item.Namespace)
		ctx := logger.AddToContext(ctx, l)
		if !instance.DeletionTimestamp.IsZero() {
			objectSelector, namespaceSelector := item.ScrapeSelectors(instance)
			opts := &k8stools.SelectorOpts{
				SelectAll:         item.Spec.SelectAllByDefault,
				NamespaceSelector: namespaceSelector,
				ObjectSelector:    objectSelector,
				DefaultNamespace:  instance.Namespace,
			}
			match, err := isSelectorsMatchesTargetCRD(ctx, r.Client, instance, item, opts)
			if err != nil {
				l.Error(err, "cannot match vmagent and vmnodescrape")
				continue
			}
			if !match {
				continue
			}
		}

		if err := vmagent.CreateOrUpdateScrapeConfig(ctx, r, item, instance); err != nil {
			continue
		}
	}

	return
}

// SetupWithManager - setups manager for VMNodeScrape
func (r *VMNodeScrapeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&vmv1beta1.VMNodeScrape{}).
		WithEventFilter(predicate.TypedGenerationChangedPredicate[client.Object]{}).
		WithOptions(getDefaultOptions()).
		Complete(r)
}
