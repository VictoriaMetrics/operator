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
var vmauthlog = logf.Log.WithName("vmauth-resource")

// SetupWebhookWithManager will setup the manager to manage the webhooks
func (r *VMAuth) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

func (r *VMAuth) sanityCheck() error {
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
