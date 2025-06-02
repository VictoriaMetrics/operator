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
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/vmanomaly"
)

// VMAnomalyStdModelReconciler reconciles a VMAnomalyStdModel object
type VMAnomalyStdModelReconciler struct {
	client.Client
	Log          logr.Logger
	OriginScheme *runtime.Scheme
}

// Init implements crdController interface
func (r *VMAnomalyStdModelReconciler) Init(rclient client.Client, l logr.Logger, sc *runtime.Scheme, _ *config.BaseOperatorConf) {
	r.Client = rclient
	r.Log = l.WithName("controller.VMAnomalyStdModel")
	r.OriginScheme = sc
}

// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmanomalystdmodels,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmanomalystdmodels/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmanomalystdmodels/finalizers,verbs=update

func (r *VMAnomalyStdModelReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	instance := &vmv1.VMAnomalyStdModel{}
	l := r.Log.WithValues("vmanomalystdmodel", req.Name, "namespace", req.Namespace)
	ctx = logger.AddToContext(ctx, l)
	defer func() {
		result, err = handleReconcileErrWithoutStatus(ctx, r.Client, instance, result, err)
	}()

	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		return result, &getError{err, "vmanomalystdmodel", req}
	}
	RegisterObjectStat(instance, "vmanomalystdmodel")
	if instance.Spec.ParsingError != "" {
		return result, &parsingError{instance.Spec.ParsingError, "vmanomalystdmodel"}
	}

	if anomalyReconcileLimit.MustThrottleReconcile() {
		return
	}

	anomalySync.Lock()
	defer anomalySync.Unlock()
	var objects vmv1.VMAnomalyList
	if err := k8stools.ListObjectsByNamespace(ctx, r.Client, config.MustGetWatchNamespaces(), func(dst *vmv1.VMAnomalyList) {
		objects.Items = append(objects.Items, dst.Items...)
	}); err != nil {
		return result, fmt.Errorf("cannot list VMAnomaly instances for VMAnomalyStdModel: %w", err)
	}
	for i := range objects.Items {
		anomaly := &objects.Items[i]
		if !anomaly.DeletionTimestamp.IsZero() || anomaly.Spec.ParsingError != "" || anomaly.IsSchedulerUnmanaged() {
			continue
		}
		l := l.WithValues("vmanomaly", anomaly.Name, "parent_namespace", anomaly.Namespace)
		ctx := logger.AddToContext(ctx, l)
		if !instance.DeletionTimestamp.IsZero() {
			match, err := isSelectorsMatchesTargetCRD(ctx, r.Client, instance, anomaly, anomaly.Spec.ModelSelectors, anomaly.Spec.SelectAllByDefault)
			if err != nil {
				l.Error(err, "cannot match VMAnomaly and VMAnomalyStdModel")
				continue
			}
			if !match {
				continue
			}
		}
		if err := vmanomaly.CreateOrUpdateConfig(ctx, r, anomaly, instance); err != nil {
			continue
		}
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *VMAnomalyStdModelReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&vmv1.VMAnomalyStdModel{}).
		WithEventFilter(predicate.TypedGenerationChangedPredicate[client.Object]{}).
		WithOptions(getDefaultOptions()).
		Complete(r)
}
