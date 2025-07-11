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
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

// SetupVMSingleWebhookWithManager will setup the manager to manage the webhooks
func SetupVMSingleWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&vmv1beta1.VMSingle{}).
		WithValidator(&VMSingleCustomValidator{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-operator-victoriametrics-com-v1beta1-vmsingle,mutating=false,failurePolicy=fail,sideEffects=None,groups=operator.victoriametrics.com,resources=vmsingles,verbs=create;update,versions=v1beta1,name=vvmsingle-v1beta1.kb.io,admissionReviewVersions=v1
type VMSingleCustomValidator struct{}

var _ admission.CustomValidator = &VMSingleCustomValidator{}

// ValidateCreate implements admission.CustomValidator so a webhook will be registered for the type
func (*VMSingleCustomValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (warnings admission.Warnings, err error) {
	r, ok := obj.(*vmv1beta1.VMSingle)
	if !ok {
		err = fmt.Errorf("BUG: unexpected type: %T", obj)
		return
	}
	if r.Spec.ParsingError != "" {
		err = errors.New(r.Spec.ParsingError)
		return
	}
	if err = r.Validate(); err != nil {
		return
	}
	if r.Spec.VMBackup != nil && r.Spec.VMBackup.AcceptEULA {
		warnings = append(warnings, "deprecated property is defined `spec.vmbackup.acceptEula`, use `spec.license.key` or `spec.license.keyRef` instead.")
		logger.WithContext(ctx).Info("deprecated property is defined `spec.vmbackup.acceptEula`, use `spec.license.key` or `spec.license.keyRef` instead.")
	}
	return
}

// ValidateUpdate implements admission.CustomValidator so a webhook will be registered for the type
func (*VMSingleCustomValidator) ValidateUpdate(ctx context.Context, _, newObj runtime.Object) (warnings admission.Warnings, err error) {
	r, ok := newObj.(*vmv1beta1.VMSingle)
	if !ok {
		err = fmt.Errorf("BUG: unexpected type: %T", newObj)
		return
	}
	if r.Spec.ParsingError != "" {
		err = errors.New(r.Spec.ParsingError)
		return
	}
	if err = r.Validate(); err != nil {
		return
	}
	if r.Spec.VMBackup != nil && r.Spec.VMBackup.AcceptEULA {
		warnings = append(warnings, "deprecated property is defined `spec.vmbackup.acceptEula`, use `spec.license.key` or `spec.license.keyRef` instead.")
		logger.WithContext(ctx).Info("deprecated property is defined `spec.vmbackup.acceptEula`, use `spec.license.key` or `spec.license.keyRef` instead.")
	}
	return
}

// ValidateDelete implements admission.CustomValidator so a webhook will be registered for the type
func (*VMSingleCustomValidator) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}
