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
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// log is for logging in this package.
var vmclusterlog = logf.Log.WithName("vmcluster-resource")

// SetupWebhookWithManager will setup the manager to manage the webhooks
func (r *VMCluster) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// +kubebuilder:webhook:path=/validate-operator-victoriametrics-com-v1beta1-vmcluster,mutating=false,failurePolicy=fail,sideEffects=None,groups=operator.victoriametrics.com,resources=vmclusters,verbs=create;update,versions=v1beta1,name=vvmcluster.kb.io,admissionReviewVersions=v1

var _ webhook.Validator = &VMCluster{}

func (r *VMCluster) sanityCheck() error {
	if r.Spec.VMSelect != nil {
		vms := r.Spec.VMSelect
		if vms.ServiceSpec != nil && vms.ServiceSpec.Name == r.GetVMSelectName() {
			return fmt.Errorf(".serviceSpec.Name cannot be equal to prefixed name=%q", r.GetVMSelectName())
		}
		if vms.HPA != nil {
			if err := vms.HPA.sanityCheck(); err != nil {
				return err
			}
		}
		if vms.StorageSpec != nil {
			vmclusterlog.Info("deprecated property is defined `vmcluster.spec.vmselect.persistentVolume`, use `storage` instead.")
		}
	}
	if r.Spec.VMInsert != nil {
		vmi := r.Spec.VMInsert
		if vmi.ServiceSpec != nil && vmi.ServiceSpec.Name == r.GetVMInsertName() {
			return fmt.Errorf(".serviceSpec.Name cannot be equal to prefixed name=%q", r.GetVMInsertName())
		}
		if vmi.HPA != nil {
			if err := vmi.HPA.sanityCheck(); err != nil {
				return err
			}
		}
	}
	if r.Spec.VMStorage != nil {
		vms := r.Spec.VMStorage
		if vms.ServiceSpec != nil && vms.ServiceSpec.Name == r.GetVMInsertName() {
			return fmt.Errorf(".serviceSpec.Name cannot be equal to prefixed name=%q", r.GetVMStorageName())
		}
		if r.Spec.VMStorage.VMBackup != nil {
			if err := r.Spec.VMStorage.VMBackup.sanityCheck(r.Spec.License); err != nil {
				return err
			}
		}
	}
	if r.Spec.RequestsLoadBalancer.Enabled {
		rlb := r.Spec.RequestsLoadBalancer.Spec
		if rlb.AdditionalServiceSpec != nil && rlb.AdditionalServiceSpec.Name == r.GetVMAuthLBName() {
			return fmt.Errorf(".serviceSpec.Name cannot be equal to prefixed name=%q", r.GetVMAuthLBName())
		}
	}

	return nil
}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *VMCluster) ValidateCreate() (admission.Warnings, error) {
	if r.Spec.ParsingError != "" {
		return nil, fmt.Errorf(r.Spec.ParsingError)
	}
	if mustSkipValidation(r) {
		return nil, nil
	}
	if err := r.sanityCheck(); err != nil {
		return nil, err
	}
	return nil, nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *VMCluster) ValidateUpdate(old runtime.Object) (admission.Warnings, error) {
	if r.Spec.ParsingError != "" {
		return nil, fmt.Errorf(r.Spec.ParsingError)
	}
	if mustSkipValidation(r) {
		return nil, nil
	}
	if err := r.sanityCheck(); err != nil {
		return nil, err
	}
	return nil, nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *VMCluster) ValidateDelete() (admission.Warnings, error) {
	return nil, nil
}
