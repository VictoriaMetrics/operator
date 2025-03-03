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
	"strconv"
	"strings"
	"sync"

	"github.com/VictoriaMetrics/VictoriaMetrics/app/vmalert/config"
	"github.com/VictoriaMetrics/VictoriaMetrics/app/vmalert/notifier"
	"github.com/VictoriaMetrics/VictoriaMetrics/app/vmalert/templates"
	"gopkg.in/yaml.v2"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// log is for logging in this package.
var vmrulelog = logf.Log.WithName("vmrule-resource")

var initVMAlertTemplatesOnce sync.Once

// SetupWebhookWithManager will setup the manager to manage the webhooks
func (r *VMRule) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// +kubebuilder:webhook:path=/validate-operator-victoriametrics-com-v1beta1-vmrule,mutating=false,failurePolicy=fail,sideEffects=None,groups=operator.victoriametrics.com,resources=vmrules,verbs=create;update,versions=v1beta1,name=vvmrule.kb.io,admissionReviewVersions=v1

// Validate performs symantic validation of object
func (r *VMRule) Validate() error {
	if mustSkipValidation(r) {
		return nil
	}
	initVMAlertTemplatesOnce.Do(func() {
		if err := templates.Load(nil, false); err != nil {
			panic(fmt.Sprintf("cannot init vmalert templates for validation: %s", err))
		}
	})
	uniqNames := make(map[string]struct{})
	var totalSize int
	for i := range r.Spec.Groups {
		// make a copy
		group := r.Spec.Groups[i].DeepCopy()
		// remove tenant from copy, it's needed to properly validate it with vmalert lib
		// since tenant is only supported at enterprise code
		if group.Tenant != "" {
			if err := validateRuleGroupTenantID(group.Tenant); err != nil {
				return fmt.Errorf("at idx=%d bad tenant=%q: %w", i, group.Tenant, err)
			}
			group.Tenant = ""
		}
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
		if err := vmalertGroup.Validate(notifier.ValidateTemplates, true); err != nil {
			return fmt.Errorf("validation failed for %s err: %w", errContext, err)
		}
	}
	if totalSize > MaxConfigMapDataSize {
		return fmt.Errorf("VMRule's content size: %d exceed single rule limit: %d", totalSize, MaxConfigMapDataSize)
	}
	return nil
}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *VMRule) ValidateCreate() (admission.Warnings, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}

	return nil, nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *VMRule) ValidateUpdate(old runtime.Object) (admission.Warnings, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}

	return nil, nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *VMRule) ValidateDelete() (admission.Warnings, error) {
	return nil, nil
}

func validateRuleGroupTenantID(id string) error {
	ids := strings.TrimSpace(string(id))
	idx := strings.Index(ids, ":")
	if idx < 0 {
		if _, err := strconv.ParseInt(ids, 10, 32); err != nil {
			return fmt.Errorf("cannot parse account_id: %q as int32, err: %w", ids, err)
		}
		return nil
	}
	aIDs := ids[:idx]
	pIDs := ids[idx+1:]
	if _, err := strconv.ParseInt(aIDs, 10, 32); err != nil {
		return fmt.Errorf("cannot parse account_id: %q as int32, err: %w", aIDs, err)
	}
	if _, err := strconv.ParseInt(pIDs, 10, 32); err != nil {
		return fmt.Errorf("cannot parse project_id: %q as int32, err: %w", pIDs, err)
	}
	return nil
}
