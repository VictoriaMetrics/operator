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

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

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
func (r *VMRuleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if vmAlertRateLimiter.MustThrottleReconcile() {
		// fast path
		return ctrl.Result{}, nil
	}
	reqLogger := r.Log.WithValues("vmrule", req.NamespacedName)
	reqLogger.Info("Reconciling VMRule")

	// Fetch the VMRule instance
	instance := &victoriametricsv1beta1.VMRule{}
	err := r.Get(ctx, req.NamespacedName, instance)
	if err != nil {
		return ctrl.Result{}, err
	}

	alertMngs := &victoriametricsv1beta1.VMAlertList{}
	reqLogger.Info("listing vmalerts")
	vmAlertSync.Lock()
	defer vmAlertSync.Unlock()

	err = r.List(ctx, alertMngs, config.MustGetNamespaceListOptions())
	if err != nil {
		reqLogger.Error(err, "cannot list vmalerts")
		return ctrl.Result{}, err
	}
	reqLogger.Info("current count of vm alerts: ", "len", len(alertMngs.Items))

	for _, vmalert := range alertMngs.Items {
		if vmalert.DeletionTimestamp != nil {
			continue
		}
		reqLogger.WithValues("vmalert", vmalert.Name)
		currVMAlert := &vmalert
		match, err := isSelectorsMatches(instance, currVMAlert, currVMAlert.Spec.RuleNamespaceSelector, currVMAlert.Spec.RuleSelector)
		if err != nil {
			reqLogger.Error(err, "cannot match vmalert and vmRule")
			continue
		}
		// fast path, not match
		if !match {
			continue
		}
		reqLogger.Info("reconciling vmalert rules")
		maps, err := factory.CreateOrUpdateRuleConfigMaps(ctx, currVMAlert, r)
		if err != nil {
			reqLogger.Error(err, "cannot update rules configmaps")
			return ctrl.Result{}, err
		}
		reqLogger.Info("created rules maps count", "count", len(maps))

		_, err = factory.CreateOrUpdateVMAlert(ctx, currVMAlert, r, r.BaseConf, maps)
		if err != nil {
			reqLogger.Error(err, "cannot trigger vmalert update, after rules changing")
			return ctrl.Result{}, nil
		}
		reqLogger.Info("reconciled vmalert rules")

	}
	reqLogger.Info("alert rule was reconciled")

	return ctrl.Result{}, nil
}

// SetupWithManager general setup method
func (r *VMRuleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&victoriametricsv1beta1.VMRule{}).
		Complete(r)
}

func isSelectorsMatches(sourceCRD, targetCRD client.Object, nsSelector, selector *v1.LabelSelector) (bool, error) {
	// in case of empty namespace object must be synchronized in any way,
	// coz we dont know source labels.
	// probably object already deleted.
	if sourceCRD.GetNamespace() == "" {
		return true, nil
	}
	if sourceCRD.GetNamespace() == targetCRD.GetNamespace() {
		return true, nil
	}
	// fast path config match all by default
	if selector == nil && nsSelector == nil {
		return true, nil
	}
	// fast path maybe namespace selector will match.
	if selector == nil {
		return true, nil
	}
	labelSelector, err := v1.LabelSelectorAsSelector(selector)
	if err != nil {
		return false, fmt.Errorf("cannot parse vmalert's RuleSelector selector as labelSelector: %w", err)
	}
	set := labels.Set(sourceCRD.GetLabels())
	// selector not match
	if !labelSelector.Matches(set) {
		return false, nil
	}
	return true, nil
}
