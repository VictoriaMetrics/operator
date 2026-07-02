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
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
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

// warnDistributedOpenShiftSC collects OpenShift security context warnings for all sub-components
// of a VMDistributed spec: VMAuth, ZoneCommon, and each individual zone.
func warnDistributedOpenShiftSC(spec *vmv1alpha1.VMDistributedSpec) admission.Warnings {
	var w admission.Warnings
	w = append(w, build.WarnOpenShiftSecurityContext(spec.VMAuth.Spec.SecurityContext)...)
	w = append(w, build.WarnOpenShiftClusterSpec(&spec.ZoneCommon.VMCluster.Spec)...)
	if s := spec.ZoneCommon.VMSingle; s != nil && s.Spec != nil {
		w = append(w, build.WarnOpenShiftSecurityContext(s.Spec.SecurityContext)...)
	}
	w = append(w, build.WarnOpenShiftSecurityContext(spec.ZoneCommon.VMAgent.Spec.SecurityContext)...)
	for i := range spec.Zones {
		z := &spec.Zones[i]
		w = append(w, build.WarnOpenShiftClusterSpec(&z.VMCluster.Spec)...)
		if s := z.VMSingle; s != nil && s.Spec != nil {
			w = append(w, build.WarnOpenShiftSecurityContext(s.Spec.SecurityContext)...)
		}
		w = append(w, build.WarnOpenShiftSecurityContext(z.VMAgent.Spec.SecurityContext)...)
	}
	return w
}

// ValidateCreate implements admission.Validator so a webhook will be registered for the type
func (*VMDistributedCustomValidator) ValidateCreate(ctx context.Context, obj *vmv1alpha1.VMDistributed) (warnings admission.Warnings, err error) {
	if obj.Status.ParsingSpecError != "" {
		err = errors.New(obj.Status.ParsingSpecError)
		return
	}

	if err = obj.Validate(); err != nil {
		return
	}

	warnings = warnDistributedOpenShiftSC(&obj.Spec)
	return
}

// ValidateUpdate implements admission.Validator so a webhook will be registered for the type
func (*VMDistributedCustomValidator) ValidateUpdate(ctx context.Context, _, newObj *vmv1alpha1.VMDistributed) (warnings admission.Warnings, err error) {
	if newObj.Status.ParsingSpecError != "" {
		err = errors.New(newObj.Status.ParsingSpecError)
		return
	}

	if err = newObj.Validate(); err != nil {
		return
	}

	warnings = warnDistributedOpenShiftSC(&newObj.Spec)
	return
}

// ValidateDelete implements admission.Validator so a webhook will be registered for the type
func (*VMDistributedCustomValidator) ValidateDelete(_ context.Context, _ *vmv1alpha1.VMDistributed) (admission.Warnings, error) {
	return nil, nil
}
