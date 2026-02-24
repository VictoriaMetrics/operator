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

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// SetupVMNodeScrapeWebhookWithManager will setup the manager to manage the webhooks
func SetupVMNodeScrapeWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &vmv1beta1.VMNodeScrape{}).
		WithValidator(&VMNodeScrapeCustomValidator{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-operator-victoriametrics-com-v1beta1-vmnodescrape,mutating=false,failurePolicy=fail,sideEffects=None,groups=operator.victoriametrics.com,resources=vmnodescrapes,verbs=create;update,versions=v1beta1,name=vvmnodescrape-v1beta1.kb.io,admissionReviewVersions=v1
type VMNodeScrapeCustomValidator struct{}

var _ admission.Validator[*vmv1beta1.VMNodeScrape] = &VMNodeScrapeCustomValidator{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (*VMNodeScrapeCustomValidator) ValidateCreate(_ context.Context, obj *vmv1beta1.VMNodeScrape) (admission.Warnings, error) {
	if obj.Spec.ParsingError != "" {
		return nil, errors.New(obj.Spec.ParsingError)
	}
	if err := obj.Validate(); err != nil {
		return nil, err
	}
	return nil, nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (*VMNodeScrapeCustomValidator) ValidateUpdate(_ context.Context, _, newObj *vmv1beta1.VMNodeScrape) (admission.Warnings, error) {
	if newObj.Spec.ParsingError != "" {
		return nil, errors.New(newObj.Spec.ParsingError)
	}
	if err := newObj.Validate(); err != nil {
		return nil, err
	}
	return nil, nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (*VMNodeScrapeCustomValidator) ValidateDelete(_ context.Context, _ *vmv1beta1.VMNodeScrape) (admission.Warnings, error) {
	return nil, nil
}
