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
	"github.com/VictoriaMetrics/operator/controllers/factory/vmalert"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// VMRuleReconciler reconciles a VMRule object
type VMRuleReconciler struct {
	client.Client
	Log          logr.Logger
	OriginScheme *runtime.Scheme
	BaseConf     *config.BaseOperatorConf
}

// Scheme implements interface.
func (r *VMRuleReconciler) Scheme() *runtime.Scheme {
	return r.OriginScheme
}

// Reconcile general reconcile method for controller
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmrules,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmrules/status,verbs=get;update;patch
func (r *VMRuleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	reqLogger := r.Log.WithValues("vmrule", req.NamespacedName)
	ctx = logger.AddToContext(ctx, reqLogger)

	defer func() {
		result, err = handleReconcileErr(ctx, r.Client, nil, result, err)
	}()

	// Fetch the VMRule instance
	instance := &victoriametricsv1beta1.VMRule{}
	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		return result, &getError{err, "vmrule", req}
	}

	RegisterObjectStat(instance, "vmrule")

	if vmAlertRateLimiter.MustThrottleReconcile() {
		// fast path
		return ctrl.Result{}, nil
	}

	vmAlertSync.Lock()
	defer vmAlertSync.Unlock()

	var objects victoriametricsv1beta1.VMAlertList
	if err := k8stools.ListObjectsByNamespace(ctx, r.Client, config.MustGetWatchNamespaces(), func(dst *victoriametricsv1beta1.VMAlertList) {
		objects.Items = append(objects.Items, dst.Items...)
	}); err != nil {
		return result, fmt.Errorf("cannot list vmauths for vmuser: %w", err)
	}

	for _, vmalertItem := range objects.Items {
		if vmalertItem.DeletionTimestamp != nil || vmalertItem.Spec.ParsingError != "" {
			continue
		}
		currVMAlert := &vmalertItem
		if !currVMAlert.Spec.SelectAllByDefault {
			match, err := isSelectorsMatchesTargetCRD(ctx, r.Client, instance, currVMAlert, currVMAlert.Spec.RuleSelector, currVMAlert.Spec.RuleNamespaceSelector)
			if err != nil {
				reqLogger.Error(err, "cannot match vmalert and vmRule")
				continue
			}
			if !match {
				continue
			}
		}
		reqLogger := reqLogger.WithValues("vmalert", currVMAlert.Name)
		ctx := logger.AddToContext(ctx, reqLogger)

		maps, err := vmalert.CreateOrUpdateRuleConfigMaps(ctx, currVMAlert, r)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("cannot update rules configmaps: %w", err)
		}

		if err := vmalert.CreateOrUpdateVMAlert(ctx, currVMAlert, r, r.BaseConf, maps); err != nil {
			return result, fmt.Errorf("cannot create or update vmalert for vmrule :%w", err)
		}

	}
	return
}

// SetupWithManager general setup method
func (r *VMRuleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&victoriametricsv1beta1.VMRule{}).
		WithOptions(getDefaultOptions()).
		Complete(r)
}
