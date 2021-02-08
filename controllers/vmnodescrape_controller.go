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
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/VictoriaMetrics/operator/internal/config"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
)

// VMNodeScrapeReconciler reconciles a VMNodeScrape object
type VMNodeScrapeReconciler struct {
	client.Client
	Log          logr.Logger
	OriginScheme *runtime.Scheme
	BaseConf     *config.BaseOperatorConf
}

// Scheme implements interface.
func (r *VMNodeScrapeReconciler) Scheme() *runtime.Scheme {
	return r.OriginScheme
}

// Reconcile - reconciles VMNodeScrape objects.
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmnodescrapes,verbs=*
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmnodescrapes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmnodescrapes/finalizers,verbs=*
func (r *VMNodeScrapeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	reqLogger := r.Log.WithValues("vmnodescrape", req.NamespacedName)

	// Fetch the VMNodeScrape instance
	instance := &operatorv1beta1.VMNodeScrape{}
	err := r.Get(ctx, req.NamespacedName, instance)
	if err != nil {
		//in case of object notfound we must update vmagents
		if !errors.IsNotFound(err) {
			// Error reading the object - requeue the request.
			return ctrl.Result{}, err
		}
	}
	vmAgentInstances := &operatorv1beta1.VMAgentList{}
	err = r.List(ctx, vmAgentInstances)
	if err != nil {
		reqLogger.Error(err, "cannot list vmagent objects")
		return ctrl.Result{}, err
	}
	reqLogger.Info("found vmagent objects ", "vmagents count: ", len(vmAgentInstances.Items))

	for _, vmagent := range vmAgentInstances.Items {
		if vmagent.DeletionTimestamp != nil {
			continue
		}
		reqLogger = reqLogger.WithValues("vmagent", vmagent.Name)
		currentVMagent := &vmagent
		match, err := isVMAgentMatchesVMNodeScrape(currentVMagent, instance)
		if err != nil {
			reqLogger.Error(err, "cannot match vmagent and vmProbe")
			continue
		}
		// fast path
		if !match {
			continue
		}
		reqLogger.Info("reconciling vmNodeScrape for vmagent")

		recon, err := factory.CreateOrUpdateVMAgent(ctx, currentVMagent, r, r.BaseConf)
		if err != nil {
			reqLogger.Error(err, "cannot create or update vmagent")
			return recon, err
		}
		reqLogger.Info("reconciled vmagent")
	}

	reqLogger.Info("reconciled VMNodeScrape")

	return ctrl.Result{}, nil
}

// SetupWithManager - setups manager for VMNodeScrape
func (r *VMNodeScrapeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorv1beta1.VMNodeScrape{}).
		Complete(r)
}

// heuristic for selector match.
func isVMAgentMatchesVMNodeScrape(currentVMAgent *operatorv1beta1.VMAgent, vmNodeScrape *operatorv1beta1.VMNodeScrape) (bool, error) {
	// fast path
	if currentVMAgent.Spec.NodeScrapeNamespaceSelector == nil && currentVMAgent.Namespace != vmNodeScrape.Namespace {
		return false, nil
	}
	// fast path config unmanaged
	if currentVMAgent.Spec.NodeScrapeSelector == nil && currentVMAgent.Spec.NodeScrapeNamespaceSelector == nil {
		return false, nil
	}
	// fast path maybe namespace selector will match.
	if currentVMAgent.Spec.NodeScrapeSelector == nil {
		return true, nil
	}
	selector, err := v1.LabelSelectorAsSelector(currentVMAgent.Spec.NodeScrapeSelector)
	if err != nil {
		return false, fmt.Errorf("cannot parse vmagent's NodeScrapeSelector selector as labelSelector: %w", err)
	}
	set := labels.Set(vmNodeScrape.Labels)
	// selector not match
	if !selector.Matches(set) {
		return false, nil
	}
	return true, nil
}
