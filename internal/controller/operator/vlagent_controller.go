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

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/vlagent"
)

// VLAgentReconciler reconciles a VLAgent object
type VLAgentReconciler struct {
	client.Client
	Log          logr.Logger
	OriginScheme *runtime.Scheme
	BaseConf     *config.BaseOperatorConf
}

// Init implements crdController interface
func (r *VLAgentReconciler) Init(rclient client.Client, l logr.Logger, sc *runtime.Scheme, cf *config.BaseOperatorConf) {
	r.Client = rclient
	r.Log = l.WithName("controller.VLAgent")
	r.OriginScheme = sc
	r.BaseConf = cf
}

// Reconcile general reconcile method
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vlagents,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vlagents/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vlagents/finalizers,verbs=*
// +kubebuilder:rbac:groups="",resources=pods,verbs=*
// +kubebuilder:rbac:groups="",resources=services,verbs=*
// +kubebuilder:rbac:groups="",resources=services/finalizers,verbs=*
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;create,update;list
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=*
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=*
func (r *VLAgentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	l := r.Log.WithValues("vlagent", req.Name, "namespace", req.Namespace)
	ctx = logger.AddToContext(ctx, l)
	instance := &vmv1.VLAgent{}

	defer func() {
		result, err = handleReconcileErr(ctx, r.Client, instance, result, err)
	}()
	// Fetch the VLAgent instance
	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		return result, &getError{origin: err, controller: "vlagent", requestObject: req}
	}

	RegisterObjectStat(instance, "vlagent")
	if !instance.DeletionTimestamp.IsZero() {
		if err := finalize.OnVLAgentDelete(ctx, r.Client, instance); err != nil {
			return result, err
		}
		return
	}

	if instance.Spec.ParsingError != "" {
		return result, &parsingError{instance.Spec.ParsingError, "vlagent"}
	}

	if err := finalize.AddFinalizer(ctx, r.Client, instance); err != nil {
		return result, err
	}
	r.Client.Scheme().Default(instance)

	result, err = reconcileAndTrackStatus(ctx, r.Client, instance.DeepCopy(), func() (ctrl.Result, error) {
		if err := vlagent.CreateOrUpdate(ctx, instance, r); err != nil {
			return result, err
		}
		return result, nil
	})

	if err == nil {
		result.RequeueAfter = r.BaseConf.ResyncAfterDuration()
	}

	return
}

// Scheme implements interface.
func (r *VLAgentReconciler) Scheme() *runtime.Scheme {
	return r.OriginScheme
}

// SetupWithManager general setup method
func (r *VLAgentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&vmv1.VLAgent{}).
		Owns(&appsv1.Deployment{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.ServiceAccount{}).
		WithOptions(getDefaultOptions()).
		Complete(r)
}

// IsDisabled returns true if controller should be disabled
func (*VLAgentReconciler) IsDisabled(_ *config.BaseOperatorConf, _ sets.Set[string]) bool {
	return false
}
