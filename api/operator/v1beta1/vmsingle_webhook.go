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
var vmsinglelog = logf.Log.WithName("vmsingle-resource")

var vmsingleValidator admission.CustomValidator = &VMSingle{}

// SetupWebhookWithManager will setup the manager to manage the webhooks
func (r *VMSingle) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		WithValidator(r).
		Complete()
}

// +kubebuilder:webhook:path=/validate-operator-victoriametrics-com-v1beta1-vmsingle,mutating=false,failurePolicy=fail,sideEffects=None,groups=operator.victoriametrics.com,resources=vmsingles,verbs=create;update,versions=v1beta1,name=vvmsingle.kb.io,admissionReviewVersions=v1

func (r *VMSingle) sanityCheck() error {
	if r.Spec.ServiceSpec != nil && r.Spec.ServiceSpec.Name == r.PrefixedName() {
		return fmt.Errorf("spec.serviceSpec.Name cannot be equal to prefixed name=%q", r.PrefixedName())
	}

	if r.Spec.VMBackup != nil {
		if err := r.Spec.VMBackup.sanityCheck(r.Spec.License); err != nil {
			return err
		}
	}
	if r.Spec.StorageDataPath != "" {
		if len(r.Spec.Volumes) == 0 {
			return fmt.Errorf("spec.volumes must have at least 1 value for spec.storageDataPath=%q", r.Spec.StorageDataPath)
		}
		var storageVolumeFound bool
		for _, volume := range r.Spec.Volumes {
			if volume.Name == "data" {
				storageVolumeFound = true
				break
			}
		}
		if r.Spec.VMBackup != nil {
			if !storageVolumeFound {
				return fmt.Errorf("spec.volumes must have at least 1 value with `name: data` for spec.storageDataPath=%q."+
					" It's required by operator to correctly mount backup volumeMount", r.Spec.StorageDataPath)
			}
		}
		if len(r.Spec.VolumeMounts) == 0 && !storageVolumeFound {
			return fmt.Errorf("spec.volumeMounts must have at least 1 value OR spec.volumes must have volume.name `data` for spec.storageDataPath=%q", r.Spec.StorageDataPath)
		}
	}
	return nil
}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (*VMSingle) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	r, ok := obj.(*VMSingle)
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
func (*VMSingle) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	r, ok := newObj.(*VMSingle)
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
func (*VMSingle) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}
