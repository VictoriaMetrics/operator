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

	"github.com/VictoriaMetrics/operator/controllers/factory"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
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

	// Fetch the VMRule instance
	instance := &victoriametricsv1beta1.VMRule{}
	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		return handleGetError(req, "vmrule", err)
	}

	alertMngs := &victoriametricsv1beta1.VMAlertList{}
	RegisterObjectStat(instance, "vmrule")

	if vmAlertRateLimiter.MustThrottleReconcile() {
		// fast path
		return ctrl.Result{}, nil
	}

	vmAlertSync.Lock()
	defer vmAlertSync.Unlock()

	err = r.List(ctx, alertMngs, config.MustGetNamespaceListOptions())
	if err != nil {
		return ctrl.Result{}, err
	}

	for _, vmalert := range alertMngs.Items {
		if vmalert.DeletionTimestamp != nil || vmalert.Spec.ParsingError != "" {
			continue
		}
		currVMAlert := &vmalert
		match, err := isSelectorsMatches(instance, currVMAlert, currVMAlert.Spec.RuleSelector)
		if err != nil {
			reqLogger.Error(err, "cannot match vmalert and vmRule")
			continue
		}
		// fast path, not match
		if !match {
			continue
		}
		maps, err := factory.CreateOrUpdateRuleConfigMaps(ctx, currVMAlert, r)
		if err != nil {
			reqLogger.Error(err, "cannot update rules configmaps")
			return ctrl.Result{}, err
		}

		if err := factory.CreateOrUpdateVMAlert(ctx, currVMAlert, r, r.BaseConf, maps); err != nil {
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
