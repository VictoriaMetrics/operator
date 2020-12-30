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
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
)

// VMRuleReconciler reconciles a VMRule object
type VMRuleReconciler struct {
	client.Client
	Log      logr.Logger
	Scheme   *runtime.Scheme
	BaseConf *config.BaseOperatorConf
}

// Reconcile general reconcile method for controller
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmrules,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmrules/status,verbs=get;update;patch
func (r *VMRuleReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	reqLogger := r.Log.WithValues("vmrule", req.NamespacedName)
	reqLogger.Info("Reconciling VMRule")

	// Fetch the VMRule instance
	instance := &victoriametricsv1beta1.VMRule{}
	ctx := context.Background()
	err := r.Get(ctx, req.NamespacedName, instance)
	if err != nil {
		//in case of object notfound we must update vmalerts
		if !errors.IsNotFound(err) {
			reqLogger.Error(err, "cannot get resource")
			return ctrl.Result{}, err
		}
	}

	alertMngs := &victoriametricsv1beta1.VMAlertList{}
	reqLogger.Info("listing vmalerts")
	err = r.List(ctx, alertMngs, &client.ListOptions{})
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
		match, err := isVMAlertMatchesVMRule(currVMAlert, instance)
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

// heuristic for selector match.
func isVMAlertMatchesVMRule(currentVMalert *victoriametricsv1beta1.VMAlert, vmRule *victoriametricsv1beta1.VMRule) (bool, error) {
	// fast path
	if currentVMalert.Spec.RuleNamespaceSelector == nil && currentVMalert.Namespace != vmRule.Namespace {
		return false, nil
	}
	// fast path config unmanaged
	if currentVMalert.Spec.RuleSelector == nil && currentVMalert.Spec.RuleNamespaceSelector == nil {
		return false, nil
	}
	// fast path maybe namespace selector will match.
	if currentVMalert.Spec.RuleSelector == nil {
		return true, nil
	}
	selector, err := v1.LabelSelectorAsSelector(currentVMalert.Spec.RuleSelector)
	if err != nil {
		return false, fmt.Errorf("cannot parse vmalert's RuleSelector selector as labelSelector: %w", err)
	}
	set := labels.Set(vmRule.Labels)
	// selector not match
	if !selector.Matches(set) {
		return false, nil
	}
	return true, nil
}
