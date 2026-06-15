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

// SetupScrapeConfigWebhookWithManager sets up the webhook for Prometheus ScrapeConfig
func SetupScrapeConfigWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &promv1alpha1.ScrapeConfig{}).
		WithValidator(&ScrapeConfigCustomValidator{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-monitoring-coreos-com-v1alpha1-scrapeconfig,mutating=false,failurePolicy=fail,sideEffects=None,groups=monitoring.coreos.com,resources=scrapeconfigs,verbs=create;update;patch,versions=v1alpha1,name=vscrapeconfig-v1alpha1.kb.io,admissionReviewVersions=v1
type ScrapeConfigCustomValidator struct{}

var _ admission.Validator[*promv1alpha1.ScrapeConfig] = &ScrapeConfigCustomValidator{}

func (*ScrapeConfigCustomValidator) ValidateCreate(ctx context.Context, obj *promv1alpha1.ScrapeConfig) (admission.Warnings, error) {
	vm, err := converterv1alpha1.ScrapeConfig(ctx, obj, config.MustGetBaseConfig())
	if err != nil {
		return nil, fmt.Errorf("cannot convert ScrapeConfig: %w", err)
	}
	return nil, vm.Validate()
}

func (*ScrapeConfigCustomValidator) ValidateUpdate(ctx context.Context, _, newObj *promv1alpha1.ScrapeConfig) (admission.Warnings, error) {
	vm, err := converterv1alpha1.ScrapeConfig(ctx, newObj, config.MustGetBaseConfig())
	if err != nil {
		return nil, fmt.Errorf("cannot convert ScrapeConfig: %w", err)
	}
	return nil, vm.Validate()
}

func (*ScrapeConfigCustomValidator) ValidateDelete(_ context.Context, _ *promv1alpha1.ScrapeConfig) (admission.Warnings, error) {
	return nil, nil
}
