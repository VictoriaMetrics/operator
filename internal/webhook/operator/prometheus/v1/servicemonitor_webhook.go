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

package v1

import (
	"context"

	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/converter"
)

// SetupServiceMonitorWebhookWithManager sets up the webhook for Prometheus ServiceMonitor
func SetupServiceMonitorWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &promv1.ServiceMonitor{}).
		WithValidator(&ServiceMonitorCustomValidator{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-monitoring-coreos-com-v1-servicemonitor,mutating=false,failurePolicy=fail,sideEffects=None,groups=monitoring.coreos.com,resources=servicemonitors,verbs=create;update,versions=v1,name=vservicemonitor-v1.kb.io,admissionReviewVersions=v1
type ServiceMonitorCustomValidator struct{}

var _ admission.Validator[*promv1.ServiceMonitor] = &ServiceMonitorCustomValidator{}

func (*ServiceMonitorCustomValidator) ValidateCreate(ctx context.Context, obj *promv1.ServiceMonitor) (admission.Warnings, error) {
	vm := converter.ServiceMonitor(ctx, obj, config.MustGetBaseConfig())
	return nil, vm.Validate()
}

func (*ServiceMonitorCustomValidator) ValidateUpdate(ctx context.Context, _, newObj *promv1.ServiceMonitor) (admission.Warnings, error) {
	vm := converter.ServiceMonitor(ctx, newObj, config.MustGetBaseConfig())
	return nil, vm.Validate()
}

func (*ServiceMonitorCustomValidator) ValidateDelete(_ context.Context, _ *promv1.ServiceMonitor) (admission.Warnings, error) {
	return nil, nil
}
