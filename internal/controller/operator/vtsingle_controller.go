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
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/vtsingle"
)

// VTSingleReconciler reconciles a VTSingle object
type VTSingleReconciler struct {
	client.Client
	Log          logr.Logger
	OriginScheme *runtime.Scheme
	BaseConf     *config.BaseOperatorConf
}

// Init implements crdController interface
func (r *VTSingleReconciler) Init(rclient client.Client, l logr.Logger, sc *runtime.Scheme, cf *config.BaseOperatorConf) {
	r.Client = rclient
	r.Log = l.WithName("controller.VTSingle")
	r.OriginScheme = sc
	r.BaseConf = cf
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vtsingles,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vtsingles/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vtsingles/finalizers,verbs=update
func (r *VTSingleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	l := r.Log.WithValues("vtsingle", req.Name, "namespace", req.Namespace)
	ctx = logger.AddToContext(ctx, l)
	instance := &vmv1.VTSingle{}

	defer func() {
		result, err = handleReconcileErr(ctx, r.Client, instance, result, err)
	}()

	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		return result, &getError{err, "vtsingle", req}
	}

	RegisterObjectStat(instance, "vtsingle")
	if !instance.DeletionTimestamp.IsZero() {
		if err := finalize.OnVTSingleDelete(ctx, r.Client, instance); err != nil {
			return result, err
		}
		return
	}
	if instance.Spec.ParsingError != "" {
		return result, &parsingError{instance.Spec.ParsingError, "vtsingle"}
	}
	if err := finalize.AddFinalizer(ctx, r.Client, instance); err != nil {
		return result, err
	}
	r.Client.Scheme().Default(instance)

	result, err = reconcileAndTrackStatus(ctx, r.Client, instance.DeepCopy(), func() (ctrl.Result, error) {
		if err := vtsingle.CreateOrUpdate(ctx, r, instance); err != nil {
			return result, fmt.Errorf("failed create or update vtsingle: %w", err)
		}
		return result, nil
	})

	if err == nil {
		result.RequeueAfter = r.BaseConf.ResyncAfterDuration()
	}

	return
}

// SetupWithManager sets up the controller with the Manager.
func (r *VTSingleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&vmv1.VTSingle{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		WithOptions(getDefaultOptions()).
		Complete(r)
}

// IsDisabled returns true if controller should be disabled
func (*VTSingleReconciler) IsDisabled(_ *config.BaseOperatorConf, _ sets.Set[string]) bool {
	return false
}
