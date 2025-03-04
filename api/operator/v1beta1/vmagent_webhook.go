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

	"github.com/VictoriaMetrics/VictoriaMetrics/lib/envtemplate"
	"github.com/VictoriaMetrics/VictoriaMetrics/lib/promrelabel"
	"gopkg.in/yaml.v2"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var vmagentValidator admission.CustomValidator = &VMAgent{}

// SetupWebhookWithManager will setup the manager to manage the webhooks
func (r *VMAgent) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		WithValidator(r).
		Complete()
}

// +kubebuilder:webhook:path=/validate-operator-victoriametrics-com-v1beta1-vmagent,mutating=false,failurePolicy=fail,sideEffects=None,groups=operator.victoriametrics.com,resources=vmagents,verbs=create;update,versions=v1beta1,name=vvmagent.kb.io,admissionReviewVersions=v1

func checkRelabelConfigs(src []RelabelConfig) error {
	for i := range src {
		currSrc := &src[i]
		if len(currSrc.UnderScoreSourceLabels) == 0 {
			currSrc.UnderScoreSourceLabels = currSrc.SourceLabels
		}
		if len(currSrc.UnderScoreTargetLabel) == 0 {
			currSrc.UnderScoreTargetLabel = currSrc.TargetLabel
		}
	}
	prc, err := yaml.Marshal(src)
	if err != nil {
		return fmt.Errorf("cannot parse relabelConfigs as yaml: %w", err)
	}
	prc, err = envtemplate.ReplaceBytes(prc)
	if err != nil {
		return fmt.Errorf("cannot replace envs: %w", err)
	}
	if _, err := promrelabel.ParseRelabelConfigsData(prc); err != nil {
		return fmt.Errorf("cannot parse relabelConfigs: %w", err)
	}
	return nil
}

func (r *VMAgent) sanityCheck() error {
	if r.Spec.ServiceSpec != nil && r.Spec.ServiceSpec.Name == r.PrefixedName() {
		return fmt.Errorf("spec.serviceSpec.Name cannot be equal to prefixed name=%q", r.PrefixedName())
	}
	if len(r.Spec.RemoteWrite) == 0 {
		return fmt.Errorf("spec.remoteWrite cannot be empty array, provide at least one remoteWrite")
	}
	if r.Spec.InlineScrapeConfig != "" {
		var inlineCfg yaml.MapSlice
		if err := yaml.Unmarshal([]byte(r.Spec.InlineScrapeConfig), &inlineCfg); err != nil {
			return fmt.Errorf("bad r.spec.inlineScrapeConfig it must be valid yaml, err :%w", err)
		}
	}
	if len(r.Spec.InlineRelabelConfig) > 0 {
		if err := checkRelabelConfigs(r.Spec.InlineRelabelConfig); err != nil {
			return err
		}
	}
	for idx, rw := range r.Spec.RemoteWrite {
		if rw.URL == "" {
			return fmt.Errorf("remoteWrite.url cannot be empty at idx: %d", idx)
		}
		if len(rw.InlineUrlRelabelConfig) > 0 {
			if err := checkRelabelConfigs(rw.InlineUrlRelabelConfig); err != nil {
				return fmt.Errorf("bad urlRelabelingConfig at idx: %d, err: %w", idx, err)
			}
		}
	}

	return nil
}

// ValidateCreate(_ context.Context, cr runtime.Object) implements webhook.Validator so a webhook will be registered for the type
func (r *VMAgent) ValidateCreate(_ context.Context, cr runtime.Object) (admission.Warnings, error) {
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
func (r *VMAgent) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
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
func (r *VMAgent) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}
