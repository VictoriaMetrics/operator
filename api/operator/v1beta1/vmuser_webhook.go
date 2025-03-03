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

var supportedCRDKinds = []string{
	"VMAgent", "VMAlert", "VMAlertmanager", "VMSingle", "VMCluster/vmselect", "VMCluster/vminsert", "VMCluster/vmstorage",
}

// SetupWebhookWithManager will setup the manager to manage the webhooks
func (r *VMUser) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// +kubebuilder:webhook:path=/validate-operator-victoriametrics-com-v1beta1-vmuser,mutating=false,failurePolicy=fail,sideEffects=None,groups=operator.victoriametrics.com,resources=vmusers,verbs=create;update,versions=v1beta1,name=vvmuser.kb.io,admissionReviewVersions=v1

var _ webhook.Validator = &VMUser{}

func (r *VMUser) sanityCheck() error {
	if r.Spec.UserName != nil && r.Spec.BearerToken != nil {
		return fmt.Errorf("one of spec.username and spec.bearerToken must be defined for user, got both")
	}
	if r.Spec.PasswordRef != nil && r.Spec.Password != nil {
		return fmt.Errorf("one of spec.password or spec.passwordRef must be used for user, got both")
	}
	if len(r.Spec.TargetRefs) == 0 {
		return fmt.Errorf("at least 1 TargetRef must be provided for spec.targetRefs")
	}
	isRetryCodesSet := len(r.Spec.RetryStatusCodes) > 0
	for i := range r.Spec.TargetRefs {
		targetRef := r.Spec.TargetRefs[i]
		if targetRef.CRD != nil && targetRef.Static != nil {
			return fmt.Errorf("targetRef validation failed, one of `crd` or `static` must be configured, got both")
		}
		if targetRef.CRD == nil && targetRef.Static == nil {
			return fmt.Errorf("targetRef validation failed, one of `crd` or `static` must be configured, got none")
		}
		if targetRef.Static != nil {
			if targetRef.Static.URL == "" && len(targetRef.Static.URLs) == 0 {
				return fmt.Errorf("for targetRef.static url or urls option must be set at idx=%d", i)
			}
			if targetRef.Static.URL != "" {
				if err := validateURLPrefix(targetRef.Static.URL); err != nil {
					return fmt.Errorf("incorrect static.url: %w", err)
				}
			}
			for _, staticURL := range targetRef.Static.URLs {
				if err := validateURLPrefix(staticURL); err != nil {
					return fmt.Errorf("incorrect value at static.urls: %w", err)
				}
			}
		}
		if targetRef.CRD != nil {
			if targetRef.CRD.Namespace == "" || targetRef.CRD.Name == "" {
				return fmt.Errorf("crd.name and crd.namespace cannot be empty")
			}
		}
		if err := validateHTTPHeaders(targetRef.ResponseHeaders); err != nil {
			return fmt.Errorf("failed to parse targetRef response headers :%w", err)
		}
		if err := validateHTTPHeaders(targetRef.RequestHeaders); err != nil {
			return fmt.Errorf("failed to parse targetRef headers :%w", err)
		}
		if isRetryCodesSet && len(targetRef.RetryStatusCodes) > 0 {
			return fmt.Errorf("retry_status_codes already set at VMUser.spec level")
		}
	}
	for k := range r.Spec.MetricLabels {
		if !labelNameRegexp.MatchString(k) {
			return fmt.Errorf("incorrect metricLabels key=%q, must match pattern=%q", k, labelNameRegexp)
		}
	}
	if err := validateHTTPHeaders(r.Spec.Headers); err != nil {
		return fmt.Errorf("failed to parse vmuser headers: %w", err)
	}
	if err := validateHTTPHeaders(r.Spec.ResponseHeaders); err != nil {
		return fmt.Errorf("failed to parse vmuser response headers: %w", err)
	}
	return nil
}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *VMUser) ValidateCreate() (admission.Warnings, error) {
	if mustSkipValidation(r) {
		return nil, nil
	}
	if err := r.sanityCheck(); err != nil {
		return nil, err
	}
	return nil, nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *VMUser) ValidateUpdate(old runtime.Object) (admission.Warnings, error) {
	if mustSkipValidation(r) {
		return nil, nil
	}
	if err := r.sanityCheck(); err != nil {
		return nil, err
	}
	return nil, nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *VMUser) ValidateDelete() (admission.Warnings, error) {
	return nil, nil
}
