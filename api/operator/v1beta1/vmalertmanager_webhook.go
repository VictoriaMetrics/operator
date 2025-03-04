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
	"context"
	"errors"
	"fmt"

	"github.com/prometheus/alertmanager/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// log is for logging in this package.
var vmalertmanagerlog = logf.Log.WithName("vmalertmanager-resource")

var vmAlertmanagerValidator admission.CustomValidator = &VMAlertmanager{}

// SetupWebhookWithManager will setup the manager to manage the webhooks
func (r *VMAlertmanager) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		WithValidator(r).
		Complete()
}

// +kubebuilder:webhook:path=/validate-operator-victoriametrics-com-v1beta1-vmalertmanager,mutating=false,failurePolicy=fail,sideEffects=None,groups=operator.victoriametrics.com,resources=vmalertmanagers,verbs=create;update,versions=v1beta1,name=vvmalertmanager.kb.io,admissionReviewVersions=v1

func (cr *VMAlertmanager) sanityCheck() error {
	if cr.Spec.ServiceSpec != nil && cr.Spec.ServiceSpec.Name == cr.PrefixedName() {
		return fmt.Errorf("spec.serviceSpec.Name cannot be equal to prefixed name=%q", cr.PrefixedName())
	}
	for idx, matchers := range cr.Spec.EnforcedTopRouteMatchers {
		_, err := labels.ParseMatchers(matchers)
		if err != nil {
			fmt.Errorf("incorrect EnforcedTopRouteMatchers=%q at idx=%d: %w", matchers, idx, err)
		}
	}

	if len(cr.Spec.ConfigRawYaml) > 0 {
		if err := ValidateAlertmanagerConfigSpec([]byte(cr.Spec.ConfigRawYaml)); err != nil {
			return fmt.Errorf("bad config syntax at spec.configRawYaml: %w", err)
		}
	}
	if cr.Spec.ConfigSecret == cr.ConfigSecretName() {
		return fmt.Errorf("spec.configSecret uses the same name as built-in config secret used by operator. Please change it's name")
	}
	if cr.Spec.WebConfig != nil {
		if cr.Spec.WebConfig.HTTPServerConfig != nil {
			if cr.Spec.WebConfig.HTTPServerConfig.HTTP2 && cr.Spec.WebConfig.TLSServerConfig == nil {
				return fmt.Errorf("with enabled http2 for webserver, tls_server_config must be defined")
			}
		}
		if cr.Spec.WebConfig.TLSServerConfig != nil {
			tc := cr.Spec.WebConfig.TLSServerConfig
			if tc.Certs.CertFile == "" && tc.Certs.CertSecretRef == nil {
				return fmt.Errorf("either cert_secret_ref or cert_file must be set for tls_server_config")
			}
			if tc.Certs.KeyFile == "" && tc.Certs.KeySecretRef == nil {
				return fmt.Errorf("either key_secret_ref or key_file must be set for tls_server_config")
			}
			if tc.ClientAuthType == "RequireAndVerifyClientCert" {
				if tc.ClientCAFile == "" && tc.ClientCASecretRef == nil {
					return fmt.Errorf("either client_ca_secret_ref or client_ca_file must be set for tls_server_config with enabled RequireAndVerifyClientCert")
				}
			}
		}
	}
	if cr.Spec.GossipConfig != nil {
		if cr.Spec.GossipConfig.TLSServerConfig != nil {
			tc := cr.Spec.GossipConfig.TLSServerConfig
			if tc.Certs.CertFile == "" && tc.Certs.CertSecretRef == nil {
				return fmt.Errorf("either cert_secret_ref or cert_file must be set for tls_server_config")
			}
			if tc.Certs.KeyFile == "" && tc.Certs.KeySecretRef == nil {
				return fmt.Errorf("either key_secret_ref or key_file must be set for tls_server_config")
			}
			if tc.ClientAuthType == "RequireAndVerifyClientCert" {
				if tc.ClientCAFile == "" && tc.ClientCASecretRef == nil {
					return fmt.Errorf("either client_ca_secret_ref or client_ca_file must be set for tls_server_config with enabled RequireAndVerifyClientCert")
				}
			}
		}
		if cr.Spec.GossipConfig.TLSClientConfig != nil {
			tc := cr.Spec.GossipConfig.TLSClientConfig
			if tc.Certs.CertFile == "" && tc.Certs.CertSecretRef == nil {
				return fmt.Errorf("either cert_secret_ref or cert_file must be set for tls_client_config")
			}
			if tc.Certs.KeyFile == "" && tc.Certs.KeySecretRef == nil {
				return fmt.Errorf("either key_secret_ref or key_file must be set for tls_client_config")
			}
		}
	}
	return nil
}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (cr *VMAlertmanager) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	r, ok := obj.(*VMAlertmanager)
	if !ok {
		return nil, fmt.Errorf("BUG: unexpected type: %T", obj)
	}
	vmalertmanagerlog.Info("validate create", "name", r.Name)

	if r.Spec.ParsingError != "" {
		return nil, errors.New(r.Spec.ParsingError)
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
func (cr *VMAlertmanager) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	r, ok := newObj.(*VMAlertmanager)
	if !ok {
		return nil, fmt.Errorf("BUG: unexpected type: %T", newObj)
	}

	if r.Spec.ParsingError != "" {
		return nil, errors.New(r.Spec.ParsingError)
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
func (r *VMAlertmanager) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}
