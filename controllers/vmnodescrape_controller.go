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
	Log      logr.Logger
	Scheme   *runtime.Scheme
	BaseConf *config.BaseOperatorConf
}

// Reconcile - reconciles VMNodeScrape objects.
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmnodescrapes,verbs=*
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmnodescrapes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmnodescrapes/finalizers,verbs=*
func (r *VMNodeScrapeReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
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
		reqLogger = reqLogger.WithValues("vmagent", vmagent.Name)
		reqLogger.Info("reconciling vmNodeScrape for vmagent")
		currentVMagent := &vmagent
		// selector for nodescrape is nil
		if currentVMagent.Spec.NodeScrapeSelector == nil {
			continue
		}
		selector, err := v1.LabelSelectorAsSelector(currentVMagent.Spec.NodeScrapeSelector)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("cannot parse VMAgents NodeScrape selector as labelSelector: %w", err)
		}
		set := labels.Set(instance.Labels)
		// selector not match
		// fast path.
		if !selector.Matches(set) {
			continue
		}
		recon, err := factory.CreateOrUpdateVMAgent(ctx, currentVMagent, r, r.BaseConf)
		if err != nil {
			reqLogger.Error(err, "cannot create or update vmagent")
			return recon, err
		}
		reqLogger.Info("reconciled vmagent")
	}

	reqLogger.Info("reconciled pod monitor")

	return ctrl.Result{}, nil
}

// SetupWithManager - setups manager for VMNodeScrape
func (r *VMNodeScrapeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorv1beta1.VMNodeScrape{}).
		Complete(r)
}
