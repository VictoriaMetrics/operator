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
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

var supportedCRDKinds = []string{
	"VMAgent", "VMAlert", "VMAlertmanager", "VMSingle", "VMCluster/vmselect", "VMCluster/vminsert", "VMCluster/vmstorage",
}

// log is for logging in this package.
var vmuserlog = logf.Log.WithName("vmuser-resource")

func (r *VMUser) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// +kubebuilder:webhook:verbs=create;update,admissionReviewVersions=v1,sideEffects=none,path=/validate-operator-victoriametrics-com-v1beta1-vmuser,mutating=false,failurePolicy=fail,groups=operator.victoriametrics.com,resources=vmusers,versions=v1beta1,name=vvmuser.kb.io

var _ webhook.Validator = &VMUser{}

func (cr *VMUser) sanityCheck() error {
	if cr.Spec.UserName != nil && cr.Spec.BearerToken != nil {
		return fmt.Errorf("one of spec.username and spec.bearerToken must be defined for user, got both")
	}
	if cr.Spec.PasswordRef != nil && cr.Spec.Password != nil {
		return fmt.Errorf("one of spec.password or spec.passwordRef must be used for user, got both")
	}
	for i := range cr.Spec.TargetRefs {
		targetRef := cr.Spec.TargetRefs[i]
		if targetRef.CRD != nil && targetRef.Static != nil {
			return fmt.Errorf("targetRef validation failed, one of `crd` or `static` must be configured, got both")
		}
		if targetRef.CRD == nil && targetRef.Static == nil {
			return fmt.Errorf("targetRef validation failed, one of `crd` or `static` must be configured, got none")
		}
		if targetRef.CRD != nil {
			switch targetRef.CRD.Kind {
			case "VMAgent", "VMAlert", "VMAlertmanager", "VMSingle", "VMCluster/vmselect", "VMCluster/vminsert", "VMCluster/vmstorage":
			default:
				return fmt.Errorf("unsupported crd.kind for target ref, got: `%s`, want one of: `%s`", targetRef.CRD.Kind, strings.Join(supportedCRDKinds, ","))
			}
			if targetRef.CRD.Namespace == "" || targetRef.CRD.Name == "" {
				return fmt.Errorf("crd.name and crd.namespace cannot be empty")
			}
		}
	}
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
