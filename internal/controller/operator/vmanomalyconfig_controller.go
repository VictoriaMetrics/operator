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
	"k8s.io/apimachinery/pkg/util/sets"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/vmanomaly"
)

// VMAnomalyConfigReconciler reconciles a VMAnomalyConfig object
type VMAnomalyConfigReconciler struct {
	client.Client
	Log          logr.Logger
	OriginScheme *runtime.Scheme
	BaseConf     *config.BaseOperatorConf
}

// Init implements crdController interface
func (r *VMAnomalyConfigReconciler) Init(rclient client.Client, l logr.Logger, sc *runtime.Scheme, cf *config.BaseOperatorConf) {
	r.Client = rclient
	r.Log = l.WithName("controller.VMAnomalyConfig")
	r.OriginScheme = sc
	r.BaseConf = cf
}

// Scheme implements interface.
func (r *VMAnomalyConfigReconciler) Scheme() *runtime.Scheme {
	return r.OriginScheme
}

// Reconcile general reconcile method for controller
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmanomalyconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmanomalyconfigs/status,verbs=get;update;patch
func (r *VMAnomalyConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	var instance vmv1.VMAnomalyConfig
	l := r.Log.WithValues("vmanomalyconfig", req.Name, "namespace", req.Namespace)
	ctx = logger.AddToContext(ctx, l)
	defer func() {
		result, err = handleReconcileErrWithStatus(ctx, r.Client, &instance, result, err)
	}()

	// Fetch the VMAnomalyConfig instance
	if err = r.Get(ctx, req.NamespacedName, &instance); err != nil {
		err = &getError{err, "vmanomalyconfig", req}
		return
	}

	RegisterObjectStat(&instance, "vmanomalyconfig")

	if anomalyReconcileLimit.Throttle() {
		// fast path, rate limited
		return
	}

	anomalySync.Lock()
	defer anomalySync.Unlock()
	var objects vmv1.VMAnomalyList
	if err = k8stools.ListObjectsByNamespace(ctx, r.Client, r.BaseConf.WatchNamespaces, func(dst *vmv1.VMAnomalyList) {
		objects.Items = append(objects.Items, dst.Items...)
	}); err != nil {
		err = fmt.Errorf("cannot list vmanomalies for vmanomalyconfig: %w", err)
		return
	}

	for i := range objects.Items {
		item := &objects.Items[i]
		if !item.DeletionTimestamp.IsZero() || item.Spec.ParsingError != "" || item.IsUnmanaged() {
			continue
		}
		l := l.WithValues("vmanomaly", item.Name, "parent_namespace", item.Namespace)
		ctx := logger.AddToContext(ctx, l)
		// only check selector when deleting object,
		// since labels can be changed when updating and we can't tell if it was selected before, and we can't tell if it's creating or updating.
		if !instance.DeletionTimestamp.IsZero() {
			opts := &k8stools.SelectorOpts{
				DefaultNamespace:  instance.Namespace,
				SelectAll:         item.Spec.SelectAllByDefault,
				ObjectSelector:    item.Spec.ConfigSelector,
				NamespaceSelector: item.Spec.ConfigNamespaceSelector,
			}
			match, err := isSelectorsMatchesTargetCRD(ctx, r.Client, &instance, item, opts)
			if err != nil {
				l.Error(err, "cannot match vmanomaly and vmanomalyconfig")
				continue
			}
			if !match {
				continue
			}
		}

		if err := vmanomaly.CreateOrUpdateConfig(ctx, r, item, &instance); err != nil {
			l.Error(err, "failed to update vmanomaly config")
		}
	}
	return
}

// SetupWithManager general setup method
func (r *VMAnomalyConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&vmv1.VMAnomalyConfig{}).
		WithEventFilter(predicate.TypedGenerationChangedPredicate[client.Object]{}).
		WithOptions(getDefaultOptions()).
		Complete(r)
}

func (r *VMAnomalyConfigReconciler) IsDisabled(_ *config.BaseOperatorConf, disabledControllers sets.Set[string]) bool {
	return disabledControllers.Has("VMAnomaly")
}
