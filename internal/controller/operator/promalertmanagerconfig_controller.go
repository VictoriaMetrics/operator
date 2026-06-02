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
	promv1alpha1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	converter "github.com/VictoriaMetrics/operator/internal/controller/operator/factory/converter/v1alpha1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

// PromAlertmanagerConfigReconciler reconciles a Prometheus AlertmanagerConfig object
type PromAlertmanagerConfigReconciler struct {
	client.Client
	Log          logr.Logger
	OriginScheme *runtime.Scheme
	BaseConf     *config.BaseOperatorConf
	name         string
}

// Init implements crdController interface
func (r *PromAlertmanagerConfigReconciler) Init(name string, rclient client.Client, l logr.Logger, sc *runtime.Scheme, cf *config.BaseOperatorConf) {
	r.name = name
	r.Client = rclient
	r.Log = l.WithName("controller." + name)
	r.OriginScheme = sc
	r.BaseConf = cf
	activeConverterWatchers.WithLabelValues(r.name).Add(1)
}

// Scheme implements interface.
func (r *PromAlertmanagerConfigReconciler) Scheme() *runtime.Scheme {
	return r.OriginScheme
}

// Reconcile general reconcile method for controller
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=alertmanagerconfigs,verbs=get;list;watch
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=alertmanagerconfigs/status,verbs=get;update;patch
func (r *PromAlertmanagerConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	l := r.Log.WithValues("alertmanagerconfig", req.Name, "namespace", req.Namespace)
	var instance promv1alpha1.AlertmanagerConfig
	ctx = logger.AddToContext(ctx, l)
	defer func() {
		result, err = handleReconcileErr(ctx, r.Client, &instance, result, err)
	}()

	// Fetch the PromAlertmanagerConfig instance
	if err = r.Get(ctx, req.NamespacedName, &instance); err != nil {
		err = &getError{err, r.name, req}
		return
	}

	RegisterObjectStat(&instance, r.name)
	var cr *vmv1beta1.VMAlertmanagerConfig
	if cr, err = converter.AlertmanagerConfig(&instance, r.BaseConf); err != nil {
		err = &parsingError{err.Error(), r.name}
		return
	}
	var owner *metav1.OwnerReference
	if len(cr.OwnerReferences) > 0 {
		owner = &cr.OwnerReferences[0]
	}

	if err = reconcile.VMAlertmanagerConfig(ctx, r.Client, cr, nil, owner, true); err != nil {
		l.Error(err, "failed to reconcile VMAlertmanagerConfig from AlertmanagerConfig")
	}
	return
}

// SetupWithManager general setup method
func (r *PromAlertmanagerConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&promv1alpha1.AlertmanagerConfig{}).
		WithOptions(getDefaultOptions()).
		Complete(r)
}

// IsDisabled returns true if controller should be disabled
func (*PromAlertmanagerConfigReconciler) IsDisabled(cfg *config.BaseOperatorConf, disabledControllers sets.Set[string]) bool {
	return disabledControllers.HasAny("VMAlertmanager", "VMAlertmanagerConfig") || !cfg.EnabledPrometheusConverter.AlertmanagerConfig
}
