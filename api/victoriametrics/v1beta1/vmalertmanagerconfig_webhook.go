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
)

// log is for logging in this package.
var vmalertmanagerconfiglog = logf.Log.WithName("vmalertmanagerconfig-resource")

func (r *VMAlertmanagerConfig) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

func (amc *VMAlertmanagerConfig) Validate() error {
	validateSpec := amc.DeepCopy()
	if err := parseNestedRoutes(validateSpec.Spec.Route); err != nil {
		return fmt.Errorf("cannot parse nested route for alertmanager config err: %w", err)
	}
	return nil
}

// +kubebuilder:webhook:verbs=create;update,admissionReviewVersions=v1,sideEffects=none,path=/validate-operator-victoriametrics-com-v1beta1-vmalertmanagerconfig,mutating=false,failurePolicy=fail,groups=operator.victoriametrics.com,resources=vmalertmanagerconfigs,versions=v1beta1,name=vvmalertmanagerconfig.kb.io

var _ webhook.Validator = &VMAlertmanagerConfig{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *VMAlertmanagerConfig) ValidateCreate() error {
	if r.Spec.ParsingError != "" {
		return fmt.Errorf(r.Spec.ParsingError)
	}
	if mustSkipValidation(r) {
		return nil
	}
	if err := r.Validate(); err != nil {
		return err
	}
	return nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *VMAlertmanagerConfig) ValidateUpdate(old runtime.Object) error {
	if r.Spec.ParsingError != "" {
		return fmt.Errorf(r.Spec.ParsingError)
	}
	if mustSkipValidation(r) {
		return nil
	}
	if err := r.Validate(); err != nil {
		return err
	}
	return nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *VMAlertmanagerConfig) ValidateDelete() error {
	return nil
}
