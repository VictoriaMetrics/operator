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
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// log is for logging in this package.
var vmuserlog = logf.Log.WithName("vmuser-resource")

func (r *VMUser) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// +kubebuilder:webhook:verbs=create;update,path=/validate-operator-victoriametrics-com-v1beta1-vmuser,mutating=false,failurePolicy=fail,groups=operator.victoriametrics.com,resources=vmusers,versions=v1beta1,name=vvmuser.kb.io

var _ webhook.Validator = &VMUser{}

func (cr *VMUser) sanityCheck() error {
	return nil
}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (cr *VMUser) ValidateCreate() error {
	vmuserlog.Info("validate create", "name", cr.Name)
	if mustSkipValidation(cr) {
		return nil
	}
	if err := cr.sanityCheck(); err != nil {
		return err
	}
	return nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (cr *VMUser) ValidateUpdate(old runtime.Object) error {
	vmuserlog.Info("validate update", "name", cr.Name)
	if mustSkipValidation(cr) {
		return nil
	}
	if err := cr.sanityCheck(); err != nil {
		return err
	}
	return nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *VMUser) ValidateDelete() error {
	vmuserlog.Info("validate delete", "name", r.Name)

	return nil
}
