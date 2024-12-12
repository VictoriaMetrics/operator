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
	"regexp"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var labelNameRegexp = regexp.MustCompile("^[a-zA-Z_:.][a-zA-Z0-9_:.]*$")

// SetupWebhookWithManager will setup the manager to manage the webhooks
func (r *VMAuth) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

func (r *VMAuth) sanityCheck() error {
	if r.Spec.ServiceSpec != nil && r.Spec.ServiceSpec.Name == r.PrefixedName() {
		return fmt.Errorf("spec.serviceSpec.Name cannot be equal to prefixed name=%q", r.PrefixedName())
	}
	if r.Spec.Ingress != nil {
		// check ingress
		// TlsHosts and TlsSecretName are both needed if one of them is used
		ing := r.Spec.Ingress
		if len(ing.TlsHosts) > 0 && ing.TlsSecretName == "" {
			return fmt.Errorf("spec.ingress.tlsSecretName cannot be empty with non-empty spec.ingress.tlsHosts")
		}
		if ing.TlsSecretName != "" && len(ing.TlsHosts) == 0 {
			return fmt.Errorf("spec.ingress.tlsHosts cannot be empty with non-empty spec.ingress.tlsSecretName")
		}
	}
	if r.Spec.ConfigSecret != "" && r.Spec.ExternalConfig.SecretRef != nil {
		return fmt.Errorf("spec.configSecret and spec.externalConfig.secretRef cannot be used at the same time")
	}
	if r.Spec.ExternalConfig.SecretRef != nil && r.Spec.ExternalConfig.LocalPath != "" {
		return fmt.Errorf("at most one option can be used for externalConfig: spec.configSecret or spec.externalConfig.secretRef")
	}
	if r.Spec.ExternalConfig.SecretRef != nil {
		if r.Spec.ExternalConfig.SecretRef.Name == r.PrefixedName() {
			return fmt.Errorf("spec.externalConfig.secretRef cannot be equal to the vmauth-config-CR_NAME=%q, it's operator reserved value", r.ConfigSecretName())
		}
		if r.Spec.ExternalConfig.SecretRef.Name == "" || r.Spec.ExternalConfig.SecretRef.Key == "" {
			return fmt.Errorf("name=%q and key=%q fields must be non-empty for spec.externalConfig.secretRef",
				r.Spec.ExternalConfig.SecretRef.Name, r.Spec.ExternalConfig.SecretRef.Key)
		}
	}
	if len(r.Spec.UnauthorizedAccessConfig) > 0 && r.Spec.UnauthorizedUserAccessSpec != nil {
		return fmt.Errorf("at most one option can be used `spec.unauthorizedAccessConfig` or `spec.unauthorizedUserAccessSpec`, got both")
	}
	if len(r.Spec.UnauthorizedAccessConfig) > 0 {
		for _, urlMap := range r.Spec.UnauthorizedAccessConfig {
			if err := urlMap.Validate(); err != nil {
				return fmt.Errorf("incorrect r.spec.UnauthorizedAccessConfig: %w", err)
			}
		}
		if err := r.Spec.VMUserConfigOptions.Validate(); err != nil {
			return fmt.Errorf("incorrect r.spec UnauthorizedAccessConfig options: %w", err)
		}
	}

	if r.Spec.UnauthorizedUserAccessSpec != nil {
		if err := r.Spec.UnauthorizedUserAccessSpec.Validate(); err != nil {
			return fmt.Errorf("incorrect r.spec.UnauthorizedUserAccess syntax: %w", err)
		}
	}

	return nil
}

// +kubebuilder:webhook:path=/validate-operator-victoriametrics-com-v1beta1-vmauth,mutating=false,failurePolicy=fail,sideEffects=None,groups=operator.victoriametrics.com,resources=vmauths,verbs=create;update,versions=v1beta1,name=vvmauth.kb.io,admissionReviewVersions=v1

var _ webhook.Validator = &VMAuth{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *VMAuth) ValidateCreate() (admission.Warnings, error) {
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
func (r *VMAuth) ValidateUpdate(old runtime.Object) (admission.Warnings, error) {
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
func (r *VMAuth) ValidateDelete() (admission.Warnings, error) {
	return nil, nil
}
