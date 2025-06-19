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

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/limiter"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/vmalert"
)

var vmAlertRateLimiter = limiter.NewRateLimiter("vmalert", 5)

// VMAlertReconciler reconciles a VMAlert object
type VMAlertReconciler struct {
	client.Client
	Log          logr.Logger
	OriginScheme *runtime.Scheme
	BaseConf     *config.BaseOperatorConf
}

// Init implements crdController interface
func (r *VMAlertReconciler) Init(rclient client.Client, l logr.Logger, sc *runtime.Scheme, cf *config.BaseOperatorConf) {
	r.Client = rclient
	r.Log = l.WithName("controller.VMAlert")
	r.OriginScheme = sc
	r.BaseConf = cf
}

// Scheme implements interface.
func (r *VMAlertReconciler) Scheme() *runtime.Scheme {
	return r.OriginScheme
}

// Reconcile general reconcile method for controller
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmalerts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmalerts/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmalerts/finalizers,verbs=*
func (r *VMAlertReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, resultErr error) {
	l := r.Log.WithValues("vmalert", req.Name, "namespace", req.Namespace)
	ctx = logger.AddToContext(ctx, l)
	instance := &vmv1beta1.VMAlert{}

	defer func() {
		result, resultErr = handleReconcileErr(ctx, r.Client, instance, result, resultErr)
	}()

	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		return result, &getError{err, "vmalert", req}
	}

	RegisterObjectStat(instance, "vmalert")

	if !instance.DeletionTimestamp.IsZero() {
		if err := finalize.OnVMAlertDelete(ctx, r.Client, instance); err != nil {
			return result, err
		}
		return result, nil
	}

	if instance.Spec.ParsingError != "" {
		return result, &parsingError{instance.Spec.ParsingError, "vmalert"}
	}

	if err := finalize.AddFinalizer(ctx, r.Client, instance); err != nil {
		return result, err
	}
	r.Client.Scheme().Default(instance)

	result, resultErr = reconcileAndTrackStatus(ctx, r.Client, instance.DeepCopy(), func() (ctrl.Result, error) {
		maps, err := vmalert.CreateOrUpdateRuleConfigMaps(ctx, r, instance, nil)
		if err != nil {
			return result, err
		}
		if err := vmalert.CreateOrUpdate(ctx, instance, r, maps); err != nil {
			return result, err
		}

		return result, nil
	})
	if resultErr != nil {
		return
	}
	result.RequeueAfter = r.BaseConf.ResyncAfterDuration()
	return
}

// SetupWithManager general setup method
func (r *VMAlertReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&vmv1beta1.VMAlert{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.ServiceAccount{}).
		WithOptions(getDefaultOptions()).
		Complete(r)
}
