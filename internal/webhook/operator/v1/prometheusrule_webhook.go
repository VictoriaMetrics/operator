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

package v1

import (
	"context"
	"encoding/json"
	"fmt"

	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/converter"
)

// SetupVLClusterWebhookWithManager will setup the manager to manage the webhooks
func SetupPromethuesRuleWebhookWithManager(mgr ctrl.Manager) error {
	println("setting up")
	return ctrl.NewWebhookManagedBy(mgr).
		For(&promv1.PrometheusRule{}).
		WithValidator(&PrometheusRuleValidator{}).
		Complete()
}

type PrometheusRuleValidator struct{}

var _ admission.CustomValidator = (*PrometheusRuleValidator)(nil)

// ValidateCreate implements admission.CustomValidator so a webhook will be registered for the type
func (*PrometheusRuleValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	r, ok := obj.(*promv1.PrometheusRule)
	if !ok {
		return nil, fmt.Errorf("BUG: unexpected type: %T", obj)
	}

	vmr := converter.ConvertPromRule(r, config.MustGetBaseConfig())

	if err := vmr.Validate(); err != nil {
		return nil, err
	}

	return nil, nil
}

// ValidateUpdate implements admission.CustomValidator so a webhook will be registered for the type
func (*PrometheusRuleValidator) ValidateUpdate(_ context.Context, _, newObj runtime.Object) (admission.Warnings, error) {
	r, ok := newObj.(*promv1.PrometheusRule)
	if !ok {
		return nil, fmt.Errorf("BUG: unexpected type: %T", newObj)
	}

	vmr := converter.ConvertPromRule(r, config.MustGetBaseConfig())

	if err := vmr.Validate(); err != nil {
		return nil, err
	}
	return nil, nil
}

// ValidateDelete implements admission.CustomValidator so a webhook will be registered for the type
func (*PrometheusRuleValidator) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

func promRuleSpecToVMRule(src *promv1.PrometheusRule) (*vmv1beta1.VMRule, error) {
	var vr vmv1beta1.VMRule
	specData, err := json.Marshal(src.Spec)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(specData, &vr.Spec); err != nil {
		return nil, err
	}
	return &vr, nil
}
