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

	"gopkg.in/yaml.v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/vmanomaly/config"
)

// SetupVMAnomalyConfigWebhookWithManager will setup the manager to manage the webhooks
func SetupVMAnomalyConfigWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &vmv1.VMAnomalyConfig{}).
		WithValidator(&VMAnomalyConfigCustomValidator{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-operator-victoriametrics-com-v1-vmanomalyconfig,mutating=false,failurePolicy=fail,sideEffects=None,groups=operator.victoriametrics.com,resources=vmanomalyconfigs,verbs=create;update,versions=v1,name=vvmanomalyconfig-v1.kb.io,admissionReviewVersions=v1
type VMAnomalyConfigCustomValidator struct{}

var _ admission.Validator[*vmv1.VMAnomalyConfig] = &VMAnomalyConfigCustomValidator{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (*VMAnomalyConfigCustomValidator) ValidateCreate(_ context.Context, obj *vmv1.VMAnomalyConfig) (admission.Warnings, error) {
	var pc config.PartialConfig
	if err := yaml.Unmarshal(obj.Spec.Raw, &pc); err != nil {
		return nil, err
	}
	if err := pc.Validate(); err != nil {
		return nil, err
	}
	return nil, nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (*VMAnomalyConfigCustomValidator) ValidateUpdate(_ context.Context, _, newObj *vmv1.VMAnomalyConfig) (admission.Warnings, error) {
	var pc config.PartialConfig
	if err := yaml.Unmarshal(newObj.Spec.Raw, &pc); err != nil {
		return nil, err
	}
	if err := pc.Validate(); err != nil {
		return nil, err
	}
	return nil, nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (*VMAnomalyConfigCustomValidator) ValidateDelete(_ context.Context, _ *vmv1.VMAnomalyConfig) (admission.Warnings, error) {
	return nil, nil
}
