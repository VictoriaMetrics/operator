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

	"github.com/VictoriaMetrics/operator/controllers/factory"
	"github.com/VictoriaMetrics/operator/internal/config"
	"k8s.io/apimachinery/pkg/api/errors"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
)

// VMProbeReconciler reconciles a VMProbe object
type VMProbeReconciler struct {
	client.Client
	Log      logr.Logger
	Scheme   *runtime.Scheme
	BaseConf *config.BaseOperatorConf
}

// Reconcile - syncs VMProbe
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmprobes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmprobes/status,verbs=get;update;patch
func (r *VMProbeReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	reqLogger := r.Log.WithValues("vmprobe", req.NamespacedName)

	// Fetch the VMPodScrape instance
	instance := &operatorv1beta1.VMProbe{}
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
		// fast path unmanaged.
		if vmagent.Spec.ProbeNamespaceSelector == nil && vmagent.Spec.ProbeSelector == nil {
			continue
		}
		reqLogger = reqLogger.WithValues("vmagent", vmagent.Name)
		reqLogger.Info("reconciling probe for vmagent")
		currentVMagent := &vmagent
		recon, err := factory.CreateOrUpdateVMAgent(ctx, currentVMagent, r, r.BaseConf)
		if err != nil {
			reqLogger.Error(err, "cannot create or update vmagent")
			return recon, err
		}
		reqLogger.Info("reconciled vmagent")
	}

	reqLogger.Info("reconciled vmprobe")
	return ctrl.Result{}, nil
}

// SetupWithManager - setups VMProbe manager
func (r *VMProbeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorv1beta1.VMProbe{}).
		Complete(r)
}
