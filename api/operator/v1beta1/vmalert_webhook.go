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
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// SetupWebhookWithManager will setup the manager to manage the webhooks
func (r *VMAlert) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// +kubebuilder:webhook:path=/validate-operator-victoriametrics-com-v1beta1-vmalert,mutating=false,failurePolicy=fail,sideEffects=None,groups=operator.victoriametrics.com,resources=vmalerts,verbs=create;update,versions=v1beta1,name=vvmalert.kb.io,admissionReviewVersions=v1

var _ webhook.Validator = &VMAlert{}

func (r *VMAlert) sanityCheck() error {
	if r.Spec.Datasource.URL == "" {
		return fmt.Errorf("spec.datasource.url cannot be empty")
	}

	if r.Spec.Notifier != nil {
		if r.Spec.Notifier.URL == "" && r.Spec.Notifier.Selector == nil {
			return fmt.Errorf("spec.notifier.url and spec.notifier.selector cannot be empty at the same time, provide at least one setting")
		}
	}
	for idx, nt := range r.Spec.Notifiers {
		if nt.URL == "" && nt.Selector == nil {
			return fmt.Errorf("notifier.url is empty and selector is not set, provide at least once for spec.notifiers at idx: %d", idx)
		}
	}
	if _, ok := r.Spec.ExtraArgs["notifier.blackhole"]; !ok {
		if r.Spec.Notifier == nil && len(r.Spec.Notifiers) == 0 && r.Spec.NotifierConfigRef == nil {
			return fmt.Errorf("vmalert should have at least one notifier.url or enable `-notifier.blackhole`")
		}
	}

	return nil
}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *VMAlert) ValidateCreate() (admission.Warnings, error) {
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
func (r *VMAlert) ValidateUpdate(old runtime.Object) (admission.Warnings, error) {
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
func (r *VMAlert) ValidateDelete() (admission.Warnings, error) {
	return nil, nil
}
