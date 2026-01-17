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

	if err = validateVMDistributedSpec(&r.Spec); err != nil {
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

	if err = validateVMDistributedSpec(&r.Spec); err != nil {
		return
	}

	return
}

// ValidateDelete implements admission.CustomValidator so a webhook will be registered for the type
func (*VMDistributedCustomValidator) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

// validateVMDistributedSpec validates the VMDistributed spec
func validateVMDistributedSpec(spec *vmv1alpha1.VMDistributedSpec) error {
	// Validate VMAuth
	if spec.VMAuth.Name == "" {
		return fmt.Errorf("VMAuth.Name must be set")
	}

	// VMAgent needs to specify either Name or LabelSelector
	if spec.VMAgent.Name != "" && spec.VMAgent.LabelSelector != nil {
		return fmt.Errorf("VMAgent.Name and LabelSelector cannot be set at the same time")
	}
	if spec.VMAgent.Spec != nil && spec.VMAgent.LabelSelector != nil {
		return fmt.Errorf("VMAgent.Spec and LabelSelector cannot be set at the same time")
	}

	return validateZoneSpec(&spec.Zones)
}

// validateZoneSpec validates a single ZoneSpec
func validateZoneSpec(zone *vmv1alpha1.ZoneSpec) error {
	for _, vmClusterRefOrSpec := range zone.VMClusters {
		if err := validateVMClusterRefOrSpec(0, vmClusterRefOrSpec); err != nil {
			return err
		}
	}
	return nil
}

// validateVMClusterRefOrSpec checks for mutual exclusivity and required fields for VMClusterRefOrSpec.
// This is the webhook version of the factory validation function.
func validateVMClusterRefOrSpec(vmClusterIndex int, refOrSpec vmv1alpha1.VMClusterRefOrSpec) error {
	// Check mutual exclusivity: either Ref or Spec must be set, but not both
	if refOrSpec.Spec != nil && refOrSpec.Ref != nil {
		return fmt.Errorf("vmcluster %d must specify either Ref or Spec, not both", vmClusterIndex)
	}

	// Check that at least one of Ref or Spec is set
	if refOrSpec.Spec == nil && refOrSpec.Ref == nil {
		return fmt.Errorf("vmcluster %d must have either Ref or Spec set", vmClusterIndex)
	}

	// If Spec is provided, OverrideSpec cannot be set (OverrideSpec is only for Ref)
	if refOrSpec.Spec != nil && refOrSpec.OverrideSpec != nil {
		return fmt.Errorf("vmcluster %d cannot have both Spec and OverrideSpec set", vmClusterIndex)
	}

	// If Spec is provided, Name must be set
	if refOrSpec.Spec != nil && refOrSpec.Name == "" {
		return fmt.Errorf("Ref.Name must be set when Spec is provided at vmcluster %d", vmClusterIndex)
	}

	// If Ref is provided, Ref.Name must be set
	if refOrSpec.Ref != nil && refOrSpec.Ref.Name == "" {
		return fmt.Errorf("Ref.Name must be set for reference at vmcluster %d", vmClusterIndex)
	}

	return nil
}
