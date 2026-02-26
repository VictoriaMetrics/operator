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
	"errors"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
)

// SetupVMDistributedWebhookWithManager will setup the manager to manage the webhooks
func SetupVMDistributedWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &vmv1alpha1.VMDistributed{}).
		WithValidator(&VMDistributedCustomValidator{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-operator-victoriametrics-com-v1alpha1-vmdistributed,mutating=false,failurePolicy=fail,sideEffects=None,groups=operator.victoriametrics.com,resources=vmdistributed,verbs=create;update,versions=v1alpha1,name=vmdistributed-v1alpha1.kb.io,admissionReviewVersions=v1
type VMDistributedCustomValidator struct{}

var _ admission.Validator[*vmv1alpha1.VMDistributed] = &VMDistributedCustomValidator{}

// ValidateCreate implements admission.Validator so a webhook will be registered for the type
func (*VMDistributedCustomValidator) ValidateCreate(ctx context.Context, obj *vmv1alpha1.VMDistributed) (warnings admission.Warnings, err error) {
	if obj.Spec.ParsingError != "" {
		err = errors.New(obj.Spec.ParsingError)
		return
	}

	if err = obj.Validate(); err != nil {
		return
	}

	return
}

// ValidateUpdate implements admission.Validator so a webhook will be registered for the type
func (*VMDistributedCustomValidator) ValidateUpdate(ctx context.Context, _, newObj *vmv1alpha1.VMDistributed) (warnings admission.Warnings, err error) {
	if newObj.Spec.ParsingError != "" {
		err = errors.New(newObj.Spec.ParsingError)
		return
	}

	if err = newObj.Validate(); err != nil {
		return
	}

	return
}

// ValidateDelete implements admission.Validator so a webhook will be registered for the type
func (*VMDistributedCustomValidator) ValidateDelete(_ context.Context, _ *vmv1alpha1.VMDistributed) (admission.Warnings, error) {
	return nil, nil
}
