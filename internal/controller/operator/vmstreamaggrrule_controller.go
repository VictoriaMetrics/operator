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

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/vmagent"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/vmsingle"
)

// VMStreamAggrRuleReconciler reconciles a VMStreamAggrRule object
type VMStreamAggrRuleReconciler struct {
	client.Client
	Log          logr.Logger
	OriginScheme *runtime.Scheme
}

// Init implements crdController interface
func (r *VMStreamAggrRuleReconciler) Init(rclient client.Client, l logr.Logger, sc *runtime.Scheme, cf *config.BaseOperatorConf) {
	r.Client = rclient
	r.Log = l.WithName("controller.VMStreamAggrRule")
	r.OriginScheme = sc
}

// Scheme implements interface.
func (r *VMStreamAggrRuleReconciler) Scheme() *runtime.Scheme {
	return r.OriginScheme
}

// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmstreamaggrrules,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmstreamaggrrules/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmstreamaggrrules/finalizers,verbs=update
func (r *VMStreamAggrRuleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	instance := &vmv1.VMStreamAggrRule{}
	l := r.Log.WithValues("vmstreamaggrrule", req.Name, "namespace", req.Namespace)
	ctx = logger.AddToContext(ctx, l)

	defer func() {
		result, err = handleReconcileErrWithoutStatus(ctx, r.Client, instance, result, err)
	}()

	// Fetch the VMStreamAggrRule instance
	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		return result, &getError{err, "vmrule", req}
	}

	RegisterObjectStat(instance, "vmstreamaggrrule")

	if !agentReconcileLimit.MustThrottleReconcile() {
		agentSync.Lock()
		defer agentSync.Unlock()
		var objects vmv1beta1.VMAgentList
		if err := k8stools.ListObjectsByNamespace(ctx, r.Client, config.MustGetWatchNamespaces(), func(dst *vmv1beta1.VMAgentList) {
			objects.Items = append(objects.Items, dst.Items...)
		}); err != nil {
			return result, fmt.Errorf("cannot list vmagents for vmstreamaggrrule: %w", err)
		}

		for i := range objects.Items {
			item := &objects.Items[i]
			if item.DeletionTimestamp != nil || item.Spec.ParsingError != "" {
				continue
			}
			l := l.WithValues("vmagent", item.Name, "parent_namespace", item.Namespace)
			ctx := logger.AddToContext(ctx, l)

			// only check selector when deleting object,
			// since labels can be changed when updating and we can't tell if it was selected before, and we can't tell if it's creating or updating.
			if !instance.DeletionTimestamp.IsZero() {
				opts := &k8stools.SelectorOpts{
					NamespaceSelector: item.Spec.StreamAggrConfig.RuleNamespaceSelector,
					ObjectSelector:    item.Spec.StreamAggrConfig.RuleSelector,
					DefaultNamespace:  instance.Namespace,
				}
				match, err := isSelectorsMatchesTargetCRD(ctx, r.Client, instance, item, opts)
				if err != nil {
					l.Error(err, "cannot match vmagent and vmstreamaggrrule")
					continue
				}
				if !match {
					for _, rw := range item.Spec.RemoteWrite {
						opts.NamespaceSelector = rw.StreamAggrConfig.RuleNamespaceSelector
						opts.ObjectSelector = rw.StreamAggrConfig.RuleSelector
						match, err = isSelectorsMatchesTargetCRD(ctx, r.Client, instance, item, opts)
						if err != nil {
							l.Error(err, "cannot match vmagents remotewrite and vmstreamaggrrule")
							continue
						}
						if match {
							break
						}
					}
				}
				if !match {
					continue
				}
			}

			err := vmagent.CreateOrUpdateStreamAggrConfig(ctx, r, item, instance)
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("cannot update stream aggr rules: %w", err)
			}
		}
	}
	if !singleReconcileLimit.MustThrottleReconcile() {
		singleSync.Lock()
		defer singleSync.Unlock()
		var objects vmv1beta1.VMSingleList
		if err := k8stools.ListObjectsByNamespace(ctx, r.Client, config.MustGetWatchNamespaces(), func(dst *vmv1beta1.VMSingleList) {
			objects.Items = append(objects.Items, dst.Items...)
		}); err != nil {
			return result, fmt.Errorf("cannot list vmsingles for vmstreamaggrrule: %w", err)
		}

		for i := range objects.Items {
			item := &objects.Items[i]
			if item.DeletionTimestamp != nil || item.Spec.ParsingError != "" {
				continue
			}
			l := l.WithValues("vmsingle", item.Name, "parent_namespace", item.Namespace)
			ctx := logger.AddToContext(ctx, l)

			// only check selector when deleting object,
			// since labels can be changed when updating and we can't tell if it was selected before, and we can't tell if it's creating or updating.
			if !instance.DeletionTimestamp.IsZero() {
				opts := &k8stools.SelectorOpts{
					NamespaceSelector: item.Spec.StreamAggrConfig.RuleNamespaceSelector,
					ObjectSelector:    item.Spec.StreamAggrConfig.RuleSelector,
					DefaultNamespace:  instance.Namespace,
				}
				match, err := isSelectorsMatchesTargetCRD(ctx, r.Client, instance, item, opts)
				if err != nil {
					l.Error(err, "cannot match vmsingle and vmstreamaggrrule")
					continue
				}
				if !match {
					continue
				}
			}

			err := vmsingle.CreateOrUpdateStreamAggrConfig(ctx, r, item, instance)
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("cannot update stream aggr rules: %w", err)
			}
		}
	}
	return
}

// SetupWithManager sets up the controller with the Manager.
func (r *VMStreamAggrRuleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&vmv1.VMStreamAggrRule{}).
		WithEventFilter(predicate.TypedGenerationChangedPredicate[client.Object]{}).
		WithOptions(getDefaultOptions()).
		Complete(r)
}
