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
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/vmagent"
)

var (
	agentSync           sync.RWMutex
	agentReconcileLimit = limiter.NewReconcileRateLimiter("vmagent", 5)
)

// VMAgentReconciler reconciles a VMAgent object
type VMAgentReconciler struct {
	client.Client
	Log          logr.Logger
	OriginScheme *runtime.Scheme
	BaseConf     *config.BaseOperatorConf
	name         string
}

// Init implements crdController interface
func (r *VMAgentReconciler) Init(name string, rclient client.Client, l logr.Logger, sc *runtime.Scheme, cf *config.BaseOperatorConf) {
	r.name = strings.ToLower(name)
	r.Client = rclient
	r.Log = l.WithName("controller." + name)
	r.OriginScheme = sc
	r.BaseConf = cf
}

// Reconcile general reconcile method
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmagents,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmagents/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmagents/finalizers,verbs=*
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;watch;list
// +kubebuilder:rbac:groups="networking.k8s.io",resources=ingresses,verbs=get;watch;list
// +kubebuilder:rbac:groups="networking.k8s.io",resources=networkpolicies,verbs=*
// +kubebuilder:rbac:groups="",resources=events;endpoints;services;pods,verbs=*
// +kubebuilder:rbac:groups="",resources=endpointslices,verbs=get;watch;list
// +kubebuilder:rbac:groups="",resources=services/finalizers,verbs=*
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=*,verbs=*
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;watch;list
// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=roles;rolebindings;clusterrolebindings;clusterroles,verbs=get;create,update;list
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;create,update;list
// +kubebuilder:rbac:groups=autoscaling.k8s.io,resources=verticalpodautoscalers,verbs=*
// +kubebuilder:rbac:groups=apps,resources=deployments;statefulsets;daemonsets,verbs=*
// +kubebuilder:rbac:groups=apps,resources=statefulsets/status,verbs=get;update;patch
func (r *VMAgentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	l := r.Log.WithValues(r.name, req.Name, "namespace", req.Namespace)
	ctx = logger.AddToContext(ctx, l)
	var instance vmv1beta1.VMAgent
	defer func() {
		result, err = handleReconcileErrWithStatus(ctx, r.Client, &instance, result, err)
	}()

	// Fetch the VMAgent instance
	if err = r.Get(ctx, req.NamespacedName, &instance); err != nil {
		err = &getError{origin: err, controller: r.name, requestObject: req}
		return
	}

	if !instance.IsUnmanaged(nil) {
		agentSync.RLock()
		defer agentSync.RUnlock()
	}

	RegisterObjectStat(&instance, r.name)
	if !instance.DeletionTimestamp.IsZero() {
		err = finalize.OnVMAgentDelete(ctx, r.Client, &instance)
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
		if err := vmagent.CreateOrUpdate(ctx, &instance, r); err != nil {
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
func (r *VMAgentReconciler) Scheme() *runtime.Scheme {
	return r.OriginScheme
}

// SetupWithManager general setup method
func (r *VMAgentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&vmv1beta1.VMAgent{}).
		Owns(&appsv1.DaemonSet{}).
		Owns(&appsv1.Deployment{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.ServiceAccount{}).
		WithOptions(getDefaultOptions()).
		Complete(r)
}

// IsDisabled returns true if controller should be disabled
func (*VMAgentReconciler) IsDisabled(_ *config.BaseOperatorConf, _ sets.Set[string]) bool {
	return false
}

func collectVMAgentScrapes(l logr.Logger, ctx context.Context, rclient client.Client, watchNamespaces []string, instance client.Object) (err error) {
	if build.IsControllerDisabled("VMAgent") && agentReconcileLimit.Throttle() {
		return nil
	}
	agentSync.Lock()
	defer agentSync.Unlock()
	var objects vmv1beta1.VMAgentList
	if err = k8stools.ListObjectsByNamespace(ctx, rclient, watchNamespaces, func(dst *vmv1beta1.VMAgentList) {
		objects.Items = append(objects.Items, dst.Items...)
	}); err != nil {
		err = fmt.Errorf("cannot list VMAgents for %T: %w", instance, err)
		return
	}
	for i := range objects.Items {
		item := &objects.Items[i]
		if item.IsUnmanaged(instance) {
			continue
		}
		l := l.WithValues("vmagent", item.Name, "parent_namespace", item.Namespace)
		ctx := logger.AddToContext(ctx, l)

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
			match, err := isSelectorsMatchesTargetCRD(ctx, rclient, instance, item, opts)
			if err != nil {
				l.Error(err, fmt.Sprintf("cannot match VMAgent and %T", instance))
				continue
			}
			if !match {
				continue
			}
		}

		if configErr := vmagent.CreateOrUpdateScrapeConfig(ctx, rclient, item, instance); configErr != nil {
			l.Error(configErr, "cannot update VMAgent scrape configuration")
			err = configErr
		}
	}
	return
}
