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

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/vlogs"
)

// VLogsReconciler reconciles a VLogs object
type VLogsReconciler struct {
	client.Client
	Log          logr.Logger
	OriginScheme *runtime.Scheme
	BaseConf     *config.BaseOperatorConf
}

// Scheme implements interface.
func (r *VLogsReconciler) Scheme() *runtime.Scheme {
	return r.OriginScheme
}

// Reconcile general reconcile method for controller
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vlogs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vlogs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vlogs/finalizers,verbs=*
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=*
// +kubebuilder:rbac:groups=apps,resources=replicasets,verbs=*
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=*
func (r *VLogsReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	reqLogger := r.Log.WithValues("vlogs", req.Name, "namespace", req.Namespace)
	ctx = logger.AddToContext(ctx, reqLogger)
	instance := &vmv1beta1.VLogs{}

	defer func() {
		result, err = handleReconcileErr(ctx, r.Client, instance, result, err)
	}()

	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		return result, &getError{err, "vlogs", req}
	}

	RegisterObjectStat(instance, "vlogs")
	if !instance.DeletionTimestamp.IsZero() {
		if err := finalize.OnVLogsDelete(ctx, r.Client, instance); err != nil {
			return result, err
		}
		return
	}
	if instance.Spec.ParsingError != "" {
		return result, &parsingError{instance.Spec.ParsingError, "vlogs"}
	}
	if err := finalize.AddFinalizer(ctx, r.Client, instance); err != nil {
		return result, err
	}
	r.Client.Scheme().Default(instance)

	result, err = reconcileAndTrackStatus(ctx, r.Client, instance, func() (ctrl.Result, error) {
		if instance.Spec.Storage != nil && instance.Spec.StorageDataPath == "" {
			err = vlogs.CreateVLogsStorage(ctx, instance, r)
			if err != nil {
				return result, err
			}
		}

		if err = vlogs.CreateOrUpdateVLogs(ctx, instance, r); err != nil {
			return result, fmt.Errorf("failed create or update vlogs: %w", err)
		}

		return result, nil
	})

	result.RequeueAfter = r.BaseConf.ResyncAfterDuration()

	return
}

// SetupWithManager sets up the controller with the Manager.
func (r *VLogsReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&vmv1beta1.VLogs{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.ServiceAccount{}).
		WithOptions(getDefaultOptions()).
		Complete(r)
}
