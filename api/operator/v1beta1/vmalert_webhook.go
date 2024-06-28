package v1beta1

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// log is for logging in this package.
var vmalertlog = logf.Log.WithName("vmalert-resource")

func (r *VMAlert) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// +kubebuilder:webhook:verbs=create;update,admissionReviewVersions=v1,sideEffects=none,path=/validate-operator-victoriametrics-com-v1beta1-vmalert,mutating=false,failurePolicy=fail,groups=operator.victoriametrics.com,resources=vmalerts,versions=v1beta1,name=vvmalert.kb.io

var _ webhook.Validator = &VMAlert{}

func (r *VMAlert) sanityCheck() error {
	if r.Spec.Datasource.URL == "" {
		return fmt.Errorf("spec.datasource.url cannot be empty")
	}

	if r.Spec.Notifier != nil {
		if r.Spec.Notifier.URL == "" && r.Spec.Notifier.Selector == nil {
			return fmt.Errorf("spec.notifier.url and spec.notifier.selector cannot be empty at the same time, provide at least one setting")
		}
	}
	for idx, nt := range r.Spec.Notifiers {
		if nt.URL == "" && nt.Selector == nil {
			return fmt.Errorf("notifier.url is empty and selector is not set, provide at least once for spec.notifiers at idx: %d", idx)
		}
	}
	if _, ok := r.Spec.ExtraArgs["notifier.blackhole"]; !ok {
		if r.Spec.Notifier == nil && len(r.Spec.Notifiers) == 0 && r.Spec.NotifierConfigRef == nil {
			return fmt.Errorf("vmalert should have at least one notifier.url or enable `-notifier.blackhole`")
		}
	}
	if r.Spec.NotifierConfigRef != nil {
		if err := r.Spec.NotifierConfigRef.Validate(); err != nil {
			return err
		}
	}

	return nil
}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *VMAlert) ValidateCreate() (aw admission.Warnings, err error) {
	if r.Spec.ParsingError != "" {
		return aw, fmt.Errorf(r.Spec.ParsingError)
	}
	if mustSkipValidation(r) {
		return
	}
	if err := r.sanityCheck(); err != nil {
		return aw, err
	}

	return
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *VMAlert) ValidateUpdate(old runtime.Object) (aw admission.Warnings, err error) {
	if r.Spec.ParsingError != "" {
		return aw, fmt.Errorf(r.Spec.ParsingError)
	}
	if mustSkipValidation(r) {
		return
	}
	if err := r.sanityCheck(); err != nil {
		return aw, err
	}
	return
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *VMAlert) ValidateDelete() (aw admission.Warnings, err error) {
	// no-op
	return
}
