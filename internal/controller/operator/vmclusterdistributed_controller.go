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
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/vmdistributedcluster"
)

const (
	httpTimeout = 10 * time.Second
)

// VMDistributedClusterReconciler reconciles a VMDistributedCluster object
type VMDistributedClusterReconciler struct {
	client.Client
	BaseConf     *config.BaseOperatorConf
	Log          logr.Logger
	OriginScheme *runtime.Scheme
}

// Init implements crdController interface
func (r *VMDistributedClusterReconciler) Init(rclient client.Client, l logr.Logger, sc *runtime.Scheme, cf *config.BaseOperatorConf) {
	r.Client = rclient
	r.Log = l.WithName("controller.VMDistributedClusterReconciler")
	r.OriginScheme = sc
	r.BaseConf = cf
}

// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmdistributedclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmdistributedclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmdistributedclusters/finalizers,verbs=update
func (r *VMDistributedClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	l := r.Log.WithValues("vmdistributedcluster", req.Name, "namespace", req.Namespace)
	ctx = logger.AddToContext(ctx, l)
	instance := &vmv1alpha1.VMDistributedCluster{}

	// Handle reconcile errors
	defer func() {
		result, err = handleReconcileErr(ctx, r.Client, instance, result, err)
	}()

	// Fetch VMDistributedCluster instance
	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		return result, &getError{err, "vmdistributedcluster", req}
	}

	// Register metrics
	RegisterObjectStat(instance, "vmdistributedcluster")

	// Check if the instance is being deleted
	if !instance.DeletionTimestamp.IsZero() {
		if err := finalize.OnVMDistributedClusterDelete(ctx, r, instance); err != nil {
			return result, fmt.Errorf("cannot remove finalizer from vmdistributed: %w", err)
		}
		return result, nil
	}
	// Check parsing error
	if instance.Spec.ParsingError != "" {
		return result, &parsingError{instance.Spec.ParsingError, "vmdistributedcluster"}
	}

	// Add finalizer if necessary
	// TODO[vrutkovs]: Implement finalizer logic or remove it
	// if err := finalize.AddFinalizer(ctx, r.Client, instance); err != nil {
	// 	return result, err
	// }
	r.Client.Scheme().Default(instance)
	result, err = reconcileAndTrackStatus(ctx, r.Client, instance.DeepCopy(), func() (ctrl.Result, error) {
		if err := vmdistributedcluster.CreateOrUpdate(ctx, instance, r, r.OriginScheme, httpTimeout); err != nil {
			return result, fmt.Errorf("vmdistributedcluster %s update failed: %w", instance.Name, err)
		}

		return result, nil
	})
	if err != nil {
		return
	}
	result.RequeueAfter = r.BaseConf.ResyncAfterDuration()
	return
}

// SetupWithManager sets up the controller with the Manager.
func (r *VMDistributedClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&vmv1alpha1.VMDistributedCluster{}).
		Named("operator-VMDistributedCluster").
		Complete(r)
}
