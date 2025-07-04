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
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
)

// SetupVMStreamAggrRuleWebhookWithManager registers the webhook for VMStreamAggrRule in the manager.
func SetupVMStreamAggrRuleWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&vmv1.VMStreamAggrRule{}).
		WithValidator(&VMStreamAggrRuleCustomValidator{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-operator-victoriametrics-com-v1-vmstreamaggrrule,mutating=false,failurePolicy=fail,sideEffects=None,groups=operator.victoriametrics.com,resources=vmstreamaggrrules,verbs=create;update,versions=v1,name=vvmstreamaggrrule-v1.kb.io,admissionReviewVersions=v1
type VMStreamAggrRuleCustomValidator struct{}

var _ webhook.CustomValidator = &VMStreamAggrRuleCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type VMStreamAggrRule.
func (v *VMStreamAggrRuleCustomValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	r, ok := obj.(*vmv1.VMStreamAggrRule)
	if !ok {
		return nil, fmt.Errorf("BUG: unexpected type: %T", obj)
	}
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return nil, nil
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type VMStreamAggrRule.
func (v *VMStreamAggrRuleCustomValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	r, ok := newObj.(*vmv1.VMStreamAggrRule)
	if !ok {
		return nil, fmt.Errorf("BUG: unexpected type: %T", newObj)
	}
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return nil, nil
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type VMStreamAggrRule.
func (v *VMStreamAggrRuleCustomValidator) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}
