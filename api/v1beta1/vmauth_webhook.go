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

func (r *VMAuth) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// +kubebuilder:webhook:verbs=create;update,admissionReviewVersions=v1,sideEffects=none,path=/validate-operator-victoriametrics-com-v1beta1-vmauth,mutating=false,failurePolicy=fail,groups=operator.victoriametrics.com,resources=vmauths,versions=v1beta1,name=vvmauth.kb.io

var _ webhook.Validator = &VMAuth{}

func (cr *VMAuth) sanityCheck() error {
	if cr.Spec.Ingress != nil {
		// check ingress
		// TlsHosts and TlsSecretName are both needed if one of them is used
		ing := cr.Spec.Ingress
		if len(ing.TlsHosts) > 0 && ing.TlsSecretName == "" {
			return fmt.Errorf("spec.ingress.tlsSecretName cannot be empty with non-empty spec.ingress.tlsHosts")
		}
		if ing.TlsSecretName != "" && len(ing.TlsHosts) == 0 {
			return fmt.Errorf("spec.ingress.tlsHosts cannot be empty with non-empty spec.ingress.tlsSecretName")
		}
	}
	return nil
}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *VMAuth) ValidateCreate() (aw admission.Warnings, err error) {
	if r.Spec.ParsingError != "" {
		return aw, fmt.Errorf(r.Spec.ParsingError)
	}
	if mustSkipValidation(r) {
		return
	}
	if err := r.sanityCheck(); err != nil {
		return aw, err
	}
	return
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *VMAuth) ValidateUpdate(old runtime.Object) (aw admission.Warnings, err error) {
	if r.Spec.ParsingError != "" {
		return aw, fmt.Errorf(r.Spec.ParsingError)
	}
	if mustSkipValidation(r) {
		return
	}
	if err := r.sanityCheck(); err != nil {
		return aw, err
	}
	return
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *VMAuth) ValidateDelete() (aw admission.Warnings, err error) {
	return
}
