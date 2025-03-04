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
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// log is for logging in this package.
var vmclusterlog = logf.Log.WithName("vmcluster-resource")

var vmclusterValidator admission.CustomValidator = &VMCluster{}

// SetupWebhookWithManager will setup the manager to manage the webhooks
func (r *VMCluster) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		WithValidator(r).
		Complete()
}

// +kubebuilder:webhook:path=/validate-operator-victoriametrics-com-v1beta1-vmcluster,mutating=false,failurePolicy=fail,sideEffects=None,groups=operator.victoriametrics.com,resources=vmclusters,verbs=create;update,versions=v1beta1,name=vvmcluster.kb.io,admissionReviewVersions=v1

func (cr *VMCluster) sanityCheck() error {
	if cr.Spec.VMSelect != nil {
		vms := cr.Spec.VMSelect
		if vms.ServiceSpec != nil && vms.ServiceSpec.Name == cr.GetVMSelectName() {
			return fmt.Errorf(".serviceSpec.Name cannot be equal to prefixed name=%q", cr.GetVMSelectName())
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
	if cr.Spec.VMInsert != nil {
		vmi := cr.Spec.VMInsert
		if vmi.ServiceSpec != nil && vmi.ServiceSpec.Name == cr.GetVMInsertName() {
			return fmt.Errorf(".serviceSpec.Name cannot be equal to prefixed name=%q", cr.GetVMInsertName())
		}
		if vmi.HPA != nil {
			if err := vmi.HPA.sanityCheck(); err != nil {
				return err
			}
		}
	}
	if cr.Spec.VMStorage != nil {
		vms := cr.Spec.VMStorage
		if vms.ServiceSpec != nil && vms.ServiceSpec.Name == cr.GetVMInsertName() {
			return fmt.Errorf(".serviceSpec.Name cannot be equal to prefixed name=%q", cr.GetVMStorageName())
		}
		if cr.Spec.VMStorage.VMBackup != nil {
			if err := cr.Spec.VMStorage.VMBackup.sanityCheck(cr.Spec.License); err != nil {
				return err
			}
		}
	}
	if cr.Spec.RequestsLoadBalancer.Enabled {
		rlb := cr.Spec.RequestsLoadBalancer.Spec
		if rlb.AdditionalServiceSpec != nil && rlb.AdditionalServiceSpec.Name == cr.GetVMAuthLBName() {
			return fmt.Errorf(".serviceSpec.Name cannot be equal to prefixed name=%q", cr.GetVMAuthLBName())
		}
	}

	return nil
}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (cr *VMCluster) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	r, ok := obj.(*VMCluster)
	if !ok {
		return nil, fmt.Errorf("BUG: unexpected type: %T", obj)
	}

	if r.Spec.ParsingError != "" {
		return nil, errors.New(r.Spec.ParsingError)
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
func (cr *VMCluster) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	r, ok := newObj.(*VMCluster)
	if !ok {
		return nil, fmt.Errorf("BUG: unexpected type: %T", newObj)
	}
	if r.Spec.ParsingError != "" {
		return nil, errors.New(r.Spec.ParsingError)
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
func (r *VMCluster) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}
