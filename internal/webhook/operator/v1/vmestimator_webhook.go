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
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
)

// SetupVMEstimatorWebhookWithManager will setup the manager to manage the webhooks
func SetupVMEstimatorWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &vmv1.VMEstimator{}).
		WithValidator(&VMEstimatorCustomValidator{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-operator-victoriametrics-com-v1-vmestimator,mutating=false,failurePolicy=fail,sideEffects=None,groups=operator.victoriametrics.com,resources=vmestimators,verbs=create;update,versions=v1,name=vvmestimator-v1.kb.io,admissionReviewVersions=v1
type VMEstimatorCustomValidator struct{}

var _ admission.Validator[*vmv1.VMEstimator] = &VMEstimatorCustomValidator{}

// ValidateCreate implements admission.Validator so a webhook will be registered for the type
func (*VMEstimatorCustomValidator) ValidateCreate(_ context.Context, obj *vmv1.VMEstimator) (admission.Warnings, error) {
	if obj.Status.ParsingSpecError != "" {
		return nil, errors.New(obj.Status.ParsingSpecError)
	}
	if err := obj.Validate(); err != nil {
		return nil, err
	}
	return build.WarnOpenShiftVMEstimatorSpec(&obj.Spec), nil
}

// ValidateUpdate implements admission.Validator so a webhook will be registered for the type
func (*VMEstimatorCustomValidator) ValidateUpdate(_ context.Context, _, newObj *vmv1.VMEstimator) (admission.Warnings, error) {
	if newObj.Status.ParsingSpecError != "" && !vmv1beta1.HasUnknownFields(newObj.Status.ParsingSpecError) {
		return nil, errors.New(newObj.Status.ParsingSpecError)
	}
	if err := newObj.Validate(); err != nil {
		return nil, err
	}
	return build.WarnOpenShiftVMEstimatorSpec(&newObj.Spec), nil
}

// ValidateDelete implements admission.Validator so a webhook will be registered for the type
func (*VMEstimatorCustomValidator) ValidateDelete(_ context.Context, _ *vmv1.VMEstimator) (admission.Warnings, error) {
	return nil, nil
}
