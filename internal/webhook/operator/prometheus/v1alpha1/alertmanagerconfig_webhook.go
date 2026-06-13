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

package v1alpha1

import (
	"context"
	"fmt"

	promv1alpha1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/VictoriaMetrics/operator/internal/config"
	converterv1alpha1 "github.com/VictoriaMetrics/operator/internal/controller/operator/factory/converter/v1alpha1"
)

// SetupAlertmanagerConfigWebhookWithManager sets up the webhook for Prometheus AlertmanagerConfig
func SetupAlertmanagerConfigWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &promv1alpha1.AlertmanagerConfig{}).
		WithValidator(&AlertmanagerConfigCustomValidator{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-monitoring-coreos-com-v1alpha1-alertmanagerconfig,mutating=false,failurePolicy=fail,sideEffects=None,groups=monitoring.coreos.com,resources=alertmanagerconfigs,verbs=create;update,versions=v1alpha1,name=valertmanagerconfig-v1alpha1.kb.io,admissionReviewVersions=v1
type AlertmanagerConfigCustomValidator struct{}

var _ admission.Validator[*promv1alpha1.AlertmanagerConfig] = &AlertmanagerConfigCustomValidator{}

func (*AlertmanagerConfigCustomValidator) ValidateCreate(_ context.Context, obj *promv1alpha1.AlertmanagerConfig) (admission.Warnings, error) {
	vm, err := converterv1alpha1.AlertmanagerConfig(obj, config.MustGetBaseConfig())
	if err != nil {
		return nil, fmt.Errorf("cannot convert AlertmanagerConfig: %w", err)
	}
	return nil, vm.Validate()
}

func (*AlertmanagerConfigCustomValidator) ValidateUpdate(_ context.Context, _, newObj *promv1alpha1.AlertmanagerConfig) (admission.Warnings, error) {
	vm, err := converterv1alpha1.AlertmanagerConfig(newObj, config.MustGetBaseConfig())
	if err != nil {
		return nil, fmt.Errorf("cannot convert AlertmanagerConfig: %w", err)
	}
	return nil, vm.Validate()
}

func (*AlertmanagerConfigCustomValidator) ValidateDelete(_ context.Context, _ *promv1alpha1.AlertmanagerConfig) (admission.Warnings, error) {
	return nil, nil
}
