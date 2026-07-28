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
	"sync"

	"github.com/go-logr/logr"
	"golang.org/x/sync/errgroup"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/limiter"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/vmsingle"
)

var (
	vmsingleSync           sync.Mutex
	vmsingleReconcileLimit = limiter.NewReconcileRateLimiter("vmsingle", 5)
)

// VMSingleReconciler reconciles a VMSingle object
type VMSingleReconciler struct {
	client.Client
	Log          logr.Logger
	OriginScheme *runtime.Scheme
	BaseConf     *config.BaseOperatorConf
	name         string
}

// Init implements crdController interface
func (r *VMSingleReconciler) Init(name string, rclient client.Client, l logr.Logger, sc *runtime.Scheme, cf *config.BaseOperatorConf) {
	r.name = strings.ToLower(name)
	r.Client = rclient
	r.Log = l.WithName("controller." + name)
	r.OriginScheme = sc
	r.BaseConf = cf
}

// Scheme implements interface.
func (r *VMSingleReconciler) Scheme() *runtime.Scheme {
	return r.OriginScheme
}

// Reconcile general reconcile method for controller
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmsingles,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmsingles/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmsingles/finalizers,verbs=*
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;watch;list
// +kubebuilder:rbac:groups="",resources=nodes/metrics,verbs=get;watch;list
// +kubebuilder:rbac:groups="networking.k8s.io",resources=ingresses,verbs=get;watch;list
// +kubebuilder:rbac:groups="networking.k8s.io",resources=networkpolicies,verbs=*
// +kubebuilder:rbac:groups="",resources=events;endpoints;services;pods;persistentvolumeclaims,verbs=*
// +kubebuilder:rbac:groups="",resources=endpointslices,verbs=get;watch;list
// +kubebuilder:rbac:groups="",resources=services/finalizers,verbs=*
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=*,verbs=*
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;watch;list
// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=roles;rolebindings;clusterrolebindings;clusterroles,verbs=get;create,update;list
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;create,update;list
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=*
func (r *VMSingleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	l := r.Log.WithValues(r.name, req.Name, "namespace", req.Namespace)
	ctx = logger.AddToContext(ctx, l)
	var instance vmv1beta1.VMSingle

	defer func() {
		result, err = handleReconcileErrWithStatus(ctx, r.Client, &instance, result, err)
	}()

	if err = r.Get(ctx, req.NamespacedName, &instance); err != nil {
		err = &getError{err, r.name, req}
		return
	}

	RegisterObjectStat(&instance, r.name)
	if !instance.DeletionTimestamp.IsZero() {
		err = finalize.OnVMSingleDelete(ctx, r.Client, &instance)
		return
	}
	if instance.Status.ParsingSpecError != "" && !vmv1beta1.HasUnknownFields(instance.Status.ParsingSpecError) {
		err = &parsingError{instance.Status.ParsingSpecError, r.name}
		return
	}
	if err = finalize.AddFinalizer(ctx, r.Client, &instance); err != nil {
		return
	}
	r.Client.Scheme().Default(&instance)

	result, err = reconcileAndTrackStatus(ctx, r.Client, instance.DeepCopy(), r.name, func() (ctrl.Result, error) {
		if err := vmsingle.CreateOrUpdate(ctx, &instance, r); err != nil {
			return result, fmt.Errorf("failed create or update vmsingle: %w", err)
		}
		return result, nil
	})

	if err == nil {
		result.RequeueAfter = r.BaseConf.ResyncAfterDuration()
	}

	return
}

// SetupWithManager general setup method
func (r *VMSingleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&vmv1beta1.VMSingle{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.ServiceAccount{}).
		WithOptions(getDefaultOptions()).
		Complete(r)
}

// IsDisabled returns true if controller should be disabled
func (*VMSingleReconciler) IsDisabled(_ *config.BaseOperatorConf, _ sets.Set[string]) bool {
	return false
}

func collectVMSingleScrapes(l logr.Logger, ctx context.Context, rclient client.Client, watchNamespaces []string, instance client.Object) (err error) {
	if build.IsControllerDisabled("VMSingle") && vmsingleReconcileLimit.Throttle() {
		return nil
	}
	vmsingleSync.Lock()
	defer vmsingleSync.Unlock()

	var objects vmv1beta1.VMSingleList
	if err = k8stools.ListObjectsByNamespace(ctx, rclient, watchNamespaces, func(dst *vmv1beta1.VMSingleList) {
		objects.Items = append(objects.Items, dst.Items...)
	}); err != nil {
		err = fmt.Errorf("cannot list VMSingles for %T: %w", instance, err)
		return
	}

	var g errgroup.Group
	g.SetLimit(childReconcileConcurrencyLimit)
	for i := range objects.Items {
		item := &objects.Items[i]
		if item.IsUnmanaged(instance) {
			continue
		}
		itemLog := l.WithValues("vmsingle", item.Name, "parent_namespace", item.Namespace)
		itemCtx := logger.AddToContext(ctx, itemLog)
		// only check selector when deleting object,
		// since labels can be changed when updating and we can't tell if it was selected before, and we can't tell if it's creating or updating.
		if !instance.GetDeletionTimestamp().IsZero() {
			objectSelector, namespaceSelector := item.ScrapeSelectors(instance)
			opts := &k8stools.SelectorOpts{
				SelectAll:         item.Spec.SelectAllByDefault,
				NamespaceSelector: namespaceSelector,
				ObjectSelector:    objectSelector,
				DefaultNamespace:  instance.GetNamespace(),
			}
			match, err := isSelectorsMatchesTargetCRD(itemCtx, rclient, instance, item, opts)
			if err != nil {
				itemLog.Error(err, fmt.Sprintf("cannot match VMSingle and %T", instance))
				continue
			}
			if !match {
				continue
			}
		}
		g.Go(func() error {
			if configErr := vmsingle.CreateOrUpdateScrapeConfig(itemCtx, rclient, item, instance); configErr != nil {
				itemLog.Error(configErr, "cannot update VMSingle scrape configuration")
				return configErr
			}
			return nil
		})
	}
	err = g.Wait()
	return
}
