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
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
)

// SetupVMAnomalyOnlineMadModelWebhookWithManager registers the webhook for VMAnomalyOnlineMadModel in the manager.
func SetupVMAnomalyOnlineMadModelWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&vmv1.VMAnomalyOnlineMadModel{}).
		WithValidator(&VMAnomalyOnlineMadModelCustomValidator{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-operator-victoriametrics-com-v1-vmanomalyonlinemadmodel,mutating=false,failurePolicy=fail,sideEffects=None,groups=operator.victoriametrics.com,resources=vmanomalyonlinemadmodels,verbs=create;update,versions=v1,name=vvmanomalyonlinemadmodel-v1.kb.io,admissionReviewVersions=v1
type VMAnomalyOnlineMadModelCustomValidator struct{}

var _ webhook.CustomValidator = &VMAnomalyOnlineMadModelCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type VMAnomalyOnlineMadModel.
func (v *VMAnomalyOnlineMadModelCustomValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	r, ok := obj.(*vmv1.VMAnomalyOnlineMadModel)
	if !ok {
		return nil, fmt.Errorf("expected a VMAnomalyOnlineMadModel object but got %T", obj)
	}
	if r.Spec.ParsingError != "" {
		return nil, errors.New(r.Spec.ParsingError)
	}
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return nil, nil
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type VMAnomalyOnlineMadModel.
func (v *VMAnomalyOnlineMadModelCustomValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	r, ok := newObj.(*vmv1.VMAnomalyOnlineMadModel)
	if !ok {
		return nil, fmt.Errorf("expected a VMAnomalyOnlineMadModel object for the newObj but got %T", newObj)
	}
	if r.Spec.ParsingError != "" {
		return nil, errors.New(r.Spec.ParsingError)
	}
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return nil, nil
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type VMAnomalyOnlineMadModel.
func (v *VMAnomalyOnlineMadModelCustomValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	return nil, nil
}
