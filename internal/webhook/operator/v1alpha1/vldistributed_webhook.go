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
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
)

// SetupVLDistributedWebhookWithManager will setup the manager to manage the webhooks
func SetupVLDistributedWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &vmv1alpha1.VLDistributed{}).
		WithValidator(&VLDistributedCustomValidator{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-operator-victoriametrics-com-v1alpha1-vldistributed,mutating=false,failurePolicy=fail,sideEffects=None,groups=operator.victoriametrics.com,resources=vldistributed,verbs=create;update,versions=v1alpha1,name=vldistributed-v1alpha1.kb.io,admissionReviewVersions=v1
type VLDistributedCustomValidator struct{}

var _ admission.Validator[*vmv1alpha1.VLDistributed] = &VLDistributedCustomValidator{}

func warnVLDistributedOpenShiftSC(spec *vmv1alpha1.VLDistributedSpec) admission.Warnings {
	var w admission.Warnings
	w = append(w, build.WarnOpenShiftSecurityContext(spec.VMAuth.Spec.SecurityContext)...)
	w = append(w, build.WarnOpenShiftVLClusterSpec(&spec.ZoneCommon.VLCluster.Spec)...)
	if s := spec.ZoneCommon.VLSingle; s != nil && s.Spec != nil {
		w = append(w, build.WarnOpenShiftSecurityContext(s.Spec.SecurityContext)...)
	}
	w = append(w, build.WarnOpenShiftSecurityContext(spec.ZoneCommon.VLAgent.Spec.SecurityContext)...)
	for i := range spec.Zones {
		z := &spec.Zones[i]
		w = append(w, build.WarnOpenShiftVLClusterSpec(&z.VLCluster.Spec)...)
		if s := z.VLSingle; s != nil && s.Spec != nil {
			w = append(w, build.WarnOpenShiftSecurityContext(s.Spec.SecurityContext)...)
		}
		w = append(w, build.WarnOpenShiftSecurityContext(z.VLAgent.Spec.SecurityContext)...)
	}
	return w
}

// ValidateCreate implements admission.Validator so a webhook will be registered for the type
func (*VLDistributedCustomValidator) ValidateCreate(_ context.Context, obj *vmv1alpha1.VLDistributed) (admission.Warnings, error) {
	if obj.Status.ParsingSpecError != "" {
		return nil, errors.New(obj.Status.ParsingSpecError)
	}
	if err := obj.Validate(); err != nil {
		return nil, err
	}
	return warnVLDistributedOpenShiftSC(&obj.Spec), nil
}

// ValidateUpdate implements admission.Validator so a webhook will be registered for the type
func (*VLDistributedCustomValidator) ValidateUpdate(_ context.Context, _, newObj *vmv1alpha1.VLDistributed) (admission.Warnings, error) {
	if newObj.Status.ParsingSpecError != "" && !vmv1beta1.HasUnknownFields(newObj.Status.ParsingSpecError) {
		return nil, errors.New(newObj.Status.ParsingSpecError)
	}
	if err := newObj.Validate(); err != nil {
		return nil, err
	}
	return warnVLDistributedOpenShiftSC(&newObj.Spec), nil
}

// ValidateDelete implements admission.Validator so a webhook will be registered for the type
func (*VLDistributedCustomValidator) ValidateDelete(_ context.Context, _ *vmv1alpha1.VLDistributed) (admission.Warnings, error) {
	return nil, nil
}
