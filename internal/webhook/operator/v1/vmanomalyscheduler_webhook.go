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
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/vmanomaly/config"
)

// SetupVMAnomalySchedulerWebhookWithManager will setup the manager to manage the webhooks
func SetupVMAnomalySchedulerWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&vmv1.VMAnomalyScheduler{}).
		WithValidator(&VMAnomalySchedulerCustomValidator{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-operator-victoriametrics-com-v1-vmanomalyscheduler,mutating=false,failurePolicy=fail,sideEffects=None,groups=operator.victoriametrics.com,resources=vmanomalyschedulers,verbs=create;update,versions=v1,name=vvmanomalyscheduler-v1.kb.io,admissionReviewVersions=v1
type VMAnomalySchedulerCustomValidator struct{}

var _ admission.CustomValidator = &VMAnomalySchedulerCustomValidator{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (*VMAnomalySchedulerCustomValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	r, ok := obj.(*vmv1.VMAnomalyScheduler)
	if !ok {
		return nil, fmt.Errorf("BUG: unexpected type: %T", obj)
	}
	if r.Spec.ParsingError != "" {
		return nil, errors.New(r.Spec.ParsingError)
	}
	var s config.Scheduler
	if err := s.Validate(r.Spec.Params.Raw); err != nil {
		return nil, err
	}
	return nil, nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (*VMAnomalySchedulerCustomValidator) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	r, ok := newObj.(*vmv1.VMAnomalyScheduler)
	if !ok {
		return nil, fmt.Errorf("BUG: unexpected type: %T", newObj)
	}
	if r.Spec.ParsingError != "" {
		return nil, errors.New(r.Spec.ParsingError)
	}
	var s config.Scheduler
	if err := s.Validate(r.Spec.Params.Raw); err != nil {
		return nil, err
	}
	return nil, nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (*VMAnomalySchedulerCustomValidator) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}
