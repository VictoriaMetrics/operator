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

	"github.com/VictoriaMetrics/VictoriaMetrics/app/vmalert/config"
	"gopkg.in/yaml.v2"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// log is for logging in this package.
var vmrulelog = logf.Log.WithName("vmrule-resource")

// SetupWebhookWithManager will setup the manager to manage the webhooks
func (r *VMRule) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// +kubebuilder:webhook:path=/validate-operator-victoriametrics-com-v1beta1-vmrule,mutating=false,failurePolicy=fail,sideEffects=None,groups=operator.victoriametrics.com,resources=vmrules,verbs=create;update,versions=v1beta1,name=vvmrule.kb.io,admissionReviewVersions=v1

var _ webhook.Validator = &VMRule{}

func (r *VMRule) sanityCheck() error {
	uniqNames := make(map[string]struct{})
	var totalSize int
	for i := range r.Spec.Groups {
		group := &r.Spec.Groups[i]
		errContext := fmt.Sprintf("VMRule: %s/%s group: %s", r.Namespace, r.Name, group.Name)
		if _, ok := uniqNames[group.Name]; ok {
			return fmt.Errorf("duplicate group name: %s", errContext)
		}
		uniqNames[group.Name] = struct{}{}
		groupBytes, err := yaml.Marshal(group)
		if err != nil {
			return fmt.Errorf("cannot marshal %s, err: %w", errContext, err)
		}
		var vmalertGroup config.Group
		totalSize += len(groupBytes)
		if err := yaml.Unmarshal(groupBytes, &vmalertGroup); err != nil {
			return fmt.Errorf("cannot parse vmalert group %s, err: %w, r: \n%s", errContext, err, string(groupBytes))
		}
		if err := vmalertGroup.Validate(nil, true); err != nil {
			return fmt.Errorf("validation failed for %s err: %w", errContext, err)
		}
	}
	if totalSize > MaxConfigMapDataSize {
		return fmt.Errorf("VMRule's content size: %d exceed single rule limit: %d", totalSize, MaxConfigMapDataSize)
	}
	vmrulelog.Info("successfully validated rule", "name", r.Name)
	return nil
}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *VMRule) ValidateCreate() (admission.Warnings, error) {
	if mustSkipValidation(r) {
		return nil, nil
	}
	if err := r.sanityCheck(); err != nil {
		return nil, err
	}
	return nil, nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *VMRule) ValidateUpdate(old runtime.Object) (admission.Warnings, error) {
	if mustSkipValidation(r) {
		return nil, nil
	}
	if err := r.sanityCheck(); err != nil {
		return nil, err
	}
	return nil, nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *VMRule) ValidateDelete() (admission.Warnings, error) {
	return nil, nil
}
