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
	"errors"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
)

// SetupVMAnomalyWebhookWithManager registers the webhook for VMAnomaly in the manager.
func SetupVMAnomalyWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &vmv1.VMAnomaly{}).
		WithValidator(&VMAnomalyCustomValidator{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-operator-victoriametrics-com-v1-vmanomaly,mutating=false,failurePolicy=fail,sideEffects=None,groups=operator.victoriametrics.com,resources=vmanomalies,verbs=create;update,versions=v1,name=vvmanomaly-v1.kb.io,admissionReviewVersions=v1
type VMAnomalyCustomValidator struct{}

var _ admission.Validator[*vmv1.VMAnomaly] = &VMAnomalyCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type VMAnomaly.
func (v *VMAnomalyCustomValidator) ValidateCreate(ctx context.Context, obj *vmv1.VMAnomaly) (admission.Warnings, error) {
	if obj.Spec.ParsingError != "" {
		return nil, errors.New(obj.Spec.ParsingError)
	}
	if err := obj.Validate(); err != nil {
		return nil, err
	}
	return nil, nil
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type VMAnomaly.
func (v *VMAnomalyCustomValidator) ValidateUpdate(ctx context.Context, oldObj, newObj *vmv1.VMAnomaly) (admission.Warnings, error) {
	if newObj.Spec.ParsingError != "" {
		return nil, errors.New(newObj.Spec.ParsingError)
	}
	if err := newObj.Validate(); err != nil {
		return nil, err
	}
	return nil, nil
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type VMAnomaly.
func (v *VMAnomalyCustomValidator) ValidateDelete(_ context.Context, _ *vmv1.VMAnomaly) (admission.Warnings, error) {
	return nil, nil
}
