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

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
)

// SetupVMDistributedWebhookWithManager will setup the manager to manage the webhooks
func SetupVMDistributedWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&vmv1alpha1.VMDistributed{}).
		WithValidator(&VMDistributedCustomValidator{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-operator-victoriametrics-com-v1alpha1-vmdistributed,mutating=false,failurePolicy=fail,sideEffects=None,groups=operator.victoriametrics.com,resources=vmdistributed,verbs=create;update,versions=v1alpha1,name=vmdistributed-v1alpha1.kb.io,admissionReviewVersions=v1
type VMDistributedCustomValidator struct{}

var _ admission.CustomValidator = &VMDistributedCustomValidator{}

// ValidateCreate implements admission.CustomValidator so a webhook will be registered for the type
func (*VMDistributedCustomValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (warnings admission.Warnings, err error) {
	r, ok := obj.(*vmv1alpha1.VMDistributed)
	if !ok {
		err = fmt.Errorf("BUG: unexpected type: %T", obj)
		return
	}

	if err = r.Validate(); err != nil {
		return
	}

	return
}

// ValidateUpdate implements admission.CustomValidator so a webhook will be registered for the type
func (*VMDistributedCustomValidator) ValidateUpdate(ctx context.Context, _, newObj runtime.Object) (warnings admission.Warnings, err error) {
	r, ok := newObj.(*vmv1alpha1.VMDistributed)
	if !ok {
		err = fmt.Errorf("BUG: unexpected type: %T", newObj)
		return
	}

	if err = r.Validate(); err != nil {
		return
	}

	return
}

// ValidateDelete implements admission.CustomValidator so a webhook will be registered for the type
func (*VMDistributedCustomValidator) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}
