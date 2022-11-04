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
		ing := cr.Spec.Ingress
		if len(ing.TlsHosts) > 0 && ing.TlsSecretName == "" {
			return fmt.Errorf("spec.ingress.tlsSecretName cannot be empty with non-empty spec.ingress.tlsHosts")
		}
	}
	return nil
}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *VMAuth) ValidateCreate() error {
	if r.Spec.ParsingError != "" {
		return fmt.Errorf(r.Spec.ParsingError)
	}
	if mustSkipValidation(r) {
		return nil
	}
	if err := r.sanityCheck(); err != nil {
		return err
	}
	return nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *VMAuth) ValidateUpdate(old runtime.Object) error {
	if r.Spec.ParsingError != "" {
		return fmt.Errorf(r.Spec.ParsingError)
	}
	if mustSkipValidation(r) {
		return nil
	}
	if err := r.sanityCheck(); err != nil {
		return err
	}
	return nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *VMAuth) ValidateDelete() error {
	return nil
}
