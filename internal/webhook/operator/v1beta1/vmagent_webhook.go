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

package v1beta1

import (
	"context"
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// SetupVMAgentWebhookWithManager will setup the manager to manage the webhooks
func SetupVMAgentWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&vmv1beta1.VMAgent{}).
		WithValidator(&VMAgentCustomValidator{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-operator-victoriametrics-com-v1beta1-vmagent,mutating=false,failurePolicy=fail,sideEffects=None,groups=operator.victoriametrics.com,resources=vmagents,verbs=create;update,versions=v1beta1,name=vvmagent-v1beta1.kb.io,admissionReviewVersions=v1
type VMAgentCustomValidator struct{}

var _ admission.CustomValidator = &VMAgentCustomValidator{}

// ValidateCreate implements admission.CustomValidator so a webhook will be registered for the type
func (*VMAgentCustomValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	r, ok := obj.(*vmv1beta1.VMAgent)
	if !ok {
		return nil, fmt.Errorf("BUG: unexpected type: %T", obj)
	}
	if r.Spec.ParsingError != "" {
		return nil, errors.New(r.Spec.ParsingError)
	}
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return nil, nil
}

// ValidateUpdate implements admission.CustomValidator so a webhook will be registered for the type
func (*VMAgentCustomValidator) ValidateUpdate(_ context.Context, _, newObj runtime.Object) (admission.Warnings, error) {
	r, ok := newObj.(*vmv1beta1.VMAgent)
	if !ok {
		return nil, fmt.Errorf("BUG: unexpected type: %T", newObj)
	}
	if r.Spec.ParsingError != "" {
		return nil, errors.New(r.Spec.ParsingError)
	}
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return nil, nil
}

// ValidateDelete implements admission.CustomValidator so a webhook will be registered for the type
func (*VMAgentCustomValidator) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}
