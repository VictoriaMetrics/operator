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
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
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
	isRetryCodesSet := len(cr.Spec.UserConfigOption.RetryStatusCodes) > 0
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
		if err := parseHeaders(targetRef.URLMapCommon.ResponseHeaders); err != nil {
			return fmt.Errorf("failed to parse targetRef response headers :%w", err)
		}
		if err := parseHeaders(targetRef.URLMapCommon.RequestHeaders); err != nil {
			return fmt.Errorf("failed to parse targetRef headers :%w", err)
		}
		if isRetryCodesSet && len(targetRef.URLMapCommon.RetryStatusCodes) > 0 {
			return fmt.Errorf("retry_status_codes already set at VMUser.spec level")
		}
	}
	if err := parseHeaders(cr.Spec.UserConfigOption.Headers); err != nil {
		return fmt.Errorf("failed to parse vmuser headers: %w", err)
	}
	if err := parseHeaders(cr.Spec.UserConfigOption.ResponseHeaders); err != nil {
		return fmt.Errorf("failed to parse vmuser response headers: %w", err)
	}
	return nil
}

func parseHeaders(src []string) error {
	for idx, s := range src {
		n := strings.IndexByte(s, ':')
		if n < 0 {
			return fmt.Errorf("missing speparator char ':' between Name and Value in the header: %q at idx: %d; expected format - 'Name: Value'", s, idx)
		}
	}
	return nil
}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (cr *VMUser) ValidateCreate() (aw admission.Warnings, err error) {
	if mustSkipValidation(cr) {
		return
	}
	if err := cr.sanityCheck(); err != nil {
		return aw, err
	}
	return
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (cr *VMUser) ValidateUpdate(old runtime.Object) (aw admission.Warnings, err error) {
	if mustSkipValidation(cr) {
		return
	}
	if err := cr.sanityCheck(); err != nil {
		return aw, err
	}
	return
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *VMUser) ValidateDelete() (aw admission.Warnings, err error) {
	return
}
