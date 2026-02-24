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
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

// SetupVMServiceScrapeWebhookWithManager will setup the manager to manage the webhooks
func SetupVMServiceScrapeWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &vmv1beta1.VMServiceScrape{}).
		WithValidator(&VMServiceScrapeCustomValidator{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-operator-victoriametrics-com-v1beta1-vmservicescrape,mutating=false,failurePolicy=fail,sideEffects=None,groups=operator.victoriametrics.com,resources=vmservicescrapes,verbs=create;update,versions=v1beta1,name=vvmservicescrape-v1beta1.kb.io,admissionReviewVersions=v1
type VMServiceScrapeCustomValidator struct{}

var _ admission.Validator[*vmv1beta1.VMServiceScrape] = &VMServiceScrapeCustomValidator{}

// ValidateCreate implements admission.Validator so a webhook will be registered for the type
func (*VMServiceScrapeCustomValidator) ValidateCreate(ctx context.Context, obj *vmv1beta1.VMServiceScrape) (admission.Warnings, error) {
	if obj.Spec.ParsingError != "" {
		return nil, errors.New(obj.Spec.ParsingError)
	}
	if err := obj.Validate(); err != nil {
		return nil, err
	}
	if obj.Spec.DiscoveryRole == "endpointslices" {
		logger.WithContext(ctx).Info("deprecated discoverRole value `endpointslices`, use `endpointslice` instead.")
	}
	return nil, nil
}

// ValidateUpdate implements admission.Validator so a webhook will be registered for the type
func (*VMServiceScrapeCustomValidator) ValidateUpdate(ctx context.Context, _, newObj *vmv1beta1.VMServiceScrape) (admission.Warnings, error) {
	if newObj.Spec.ParsingError != "" {
		return nil, errors.New(newObj.Spec.ParsingError)
	}
	if err := newObj.Validate(); err != nil {
		return nil, err
	}
	if newObj.Spec.DiscoveryRole == "endpointslices" {
		logger.WithContext(ctx).Info("deprecated discoverRole value `endpointslices`, use `endpointslice` instead.")
	}
	return nil, nil
}

// ValidateDelete implements admission.Validator so a webhook will be registered for the type
func (*VMServiceScrapeCustomValidator) ValidateDelete(_ context.Context, _ *vmv1beta1.VMServiceScrape) (admission.Warnings, error) {
	return nil, nil
}
