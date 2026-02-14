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
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/vmanomaly"
)

// VMAnomalyReconciler reconciles a VMAnomaly object
type VMAnomalyReconciler struct {
	client.Client
	Log          logr.Logger
	OriginScheme *runtime.Scheme
	BaseConf     *config.BaseOperatorConf
}

// Init implements crdController interface
func (r *VMAnomalyReconciler) Init(rclient client.Client, l logr.Logger, sc *runtime.Scheme, cf *config.BaseOperatorConf) {
	r.Client = rclient
	r.Log = l.WithName("controller.VMAnomaly")
	r.OriginScheme = sc
	r.BaseConf = cf
}

// Reconcile general reconcile method
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmanomalies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmanomalies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmanomalies/finalizers,verbs=*
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=*
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=*
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;create,update;list
// +kubebuilder:rbac:groups="",resources=events,verbs=*
// +kubebuilder:rbac:groups="",resources=pods,verbs=*
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;watch;list
func (r *VMAnomalyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	l := r.Log.WithValues("vmanomaly", req.Name, "namespace", req.Namespace)
	ctx = logger.AddToContext(ctx, l)
	instance := &vmv1.VMAnomaly{}

	defer func() {
		result, err = handleReconcileErr(ctx, r.Client, instance, result, err)
	}()
	// Fetch the VMAnomaly instance
	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		return result, &getError{origin: err, controller: "vmanomaly", requestObject: req}
	}

	RegisterObjectStat(instance, "vmanomaly")
	if !instance.DeletionTimestamp.IsZero() {
		if err := finalize.OnVMAnomalyDelete(ctx, r.Client, instance); err != nil {
			return result, err
		}
		return
	}

	if instance.Spec.ParsingError != "" {
		return result, &parsingError{instance.Spec.ParsingError, "vmanomaly"}
	}

	if err := finalize.AddFinalizer(ctx, r.Client, instance); err != nil {
		return result, err
	}
	r.Client.Scheme().Default(instance)

	result, err = reconcileAndTrackStatus(ctx, r.Client, instance.DeepCopy(), func() (ctrl.Result, error) {
		if err := vmanomaly.CreateOrUpdate(ctx, instance, r); err != nil {
			return result, err
		}
		return result, nil
	})

	if err == nil {
		result.RequeueAfter = r.BaseConf.ResyncAfterDuration()
	}

	return
}

// SetupWithManager sets up the controller with the Manager.
func (r *VMAnomalyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&vmv1.VMAnomaly{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.ServiceAccount{}).
		WithOptions(getDefaultOptions()).
		Complete(r)
}
