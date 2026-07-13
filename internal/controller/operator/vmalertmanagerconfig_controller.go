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
	"strings"

	"github.com/go-logr/logr"
	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/vmalertmanager"
)

// VMAlertmanagerConfigReconciler reconciles a VMAlertmanagerConfig object
type VMAlertmanagerConfigReconciler struct {
	client.Client
	Log          logr.Logger
	OriginScheme *runtime.Scheme
	BaseConf     *config.BaseOperatorConf
	name         string
}

// Init implements crdController interface
func (r *VMAlertmanagerConfigReconciler) Init(name string, rclient client.Client, l logr.Logger, sc *runtime.Scheme, cf *config.BaseOperatorConf) {
	r.name = strings.ToLower(name)
	r.Client = rclient
	r.Log = l.WithName("controller." + name)
	r.OriginScheme = sc
	r.BaseConf = cf
}

// Scheme implements interface.
func (r *VMAlertmanagerConfigReconciler) Scheme() *runtime.Scheme {
	return r.OriginScheme
}

// Reconcile implements interface
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmalertmanagerconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmalertmanagerconfigs/status,verbs=get;update;patch
func (r *VMAlertmanagerConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	l := r.Log.WithValues(r.name, req.Name, "namespace", req.Namespace)
	var instance vmv1beta1.VMAlertmanagerConfig
	defer func() {
		result, err = handleReconcileErrWithStatus(ctx, r.Client, &instance, result, err)
	}()

	if err = r.Get(ctx, req.NamespacedName, &instance); err != nil {
		err = &getError{err, r.name, req}
		return
	}

	RegisterObjectStat(&instance, r.name)
	if instance.Status.ParsingSpecError != "" && !vmv1beta1.HasUnknownFields(instance.Status.ParsingSpecError) {
		err = &parsingError{instance.Status.ParsingSpecError, r.name}
		return
	}
	if alertmanagerReconcileLimit.Throttle() {
		return
	}

	alertmanagerSync.Lock()
	defer alertmanagerSync.Unlock()
	var objects vmv1beta1.VMAlertmanagerList
	if err = k8stools.ListObjectsByNamespace(ctx, r.Client, r.BaseConf.WatchNamespaces, func(dst *vmv1beta1.VMAlertmanagerList) {
		objects.Items = append(objects.Items, dst.Items...)
	}); err != nil {
		err = fmt.Errorf("cannot list vmalertmanagers for vmalertmanagerconfig: %w", err)
		return
	}

	var g errgroup.Group
	g.SetLimit(childReconcileConcurrencyLimit)
	for i := range objects.Items {
		item := &objects.Items[i]
		if !item.DeletionTimestamp.IsZero() || (item.Status.ParsingSpecError != "" && !vmv1beta1.HasUnknownFields(item.Status.ParsingSpecError)) || item.IsUnmanaged() {
			continue
		}

		itemLog := l.WithValues("vmalertmanager", item.Name, "parent_namespace", item.Namespace)
		itemCtx := logger.AddToContext(ctx, itemLog)

		// only check selector when deleting object,
		// since labels can be changed when updating and we can't tell if it was selected before, and we can't tell if it's creating or updating.
		if !instance.DeletionTimestamp.IsZero() {
			opts := &k8stools.SelectorOpts{
				SelectAll:         item.Spec.SelectAllByDefault,
				NamespaceSelector: item.Spec.ConfigNamespaceSelector,
				ObjectSelector:    item.Spec.ConfigSelector,
				DefaultNamespace:  instance.Namespace,
			}
			match, err := isSelectorsMatchesTargetCRD(itemCtx, r.Client, &instance, item, opts)
			if err != nil {
				itemLog.Error(err, "cannot match alertmanager against selector, probably bug")
				continue
			}
			if !match {
				continue
			}
		}
		// each VMAlertmanager's config reconcile is independent of the others - run them
		// concurrently so a slow one (e.g. waiting for its own config reload to be confirmed)
		// doesn't serialize the whole reconcile behind every other selected VMAlertmanager.
		g.Go(func() error {
			if configErr := vmalertmanager.CreateOrUpdateConfig(itemCtx, r.Client, item, &instance); configErr != nil {
				itemLog.Error(configErr, "cannot update alertmanager config")
				return configErr
			}
			return nil
		})
	}
	err = g.Wait()
	return
}

// SetupWithManager configures reconcile
func (r *VMAlertmanagerConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&vmv1beta1.VMAlertmanagerConfig{}).
		WithEventFilter(predicate.TypedGenerationChangedPredicate[client.Object]{}).
		WithOptions(getDefaultOptions()).
		Complete(r)
}

// IsDisabled returns true if controller should be disabled
func (*VMAlertmanagerConfigReconciler) IsDisabled(_ *config.BaseOperatorConf, disabledControllers sets.Set[string]) bool {
	return disabledControllers.Has("VMAlertmanager")
}
