package v1beta1

import (
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// log is for logging in this package.
var vmalertmanagerlog = logf.Log.WithName("vmalertmanager-resource")

func (r *VMAlertmanager) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// +kubebuilder:webhook:verbs=create;update,path=/validate-operator-victoriametrics-com-v1beta1-vmalertmanager,mutating=false,failurePolicy=fail,groups=operator.victoriametrics.com,resources=vmalertmanagers,versions=v1beta1,name=vvmalertmanager.kb.io

var _ webhook.Validator = &VMAlertmanager{}

func (r *VMAlertmanager) sanityCheck() error {
	return nil
}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *VMAlertmanager) ValidateCreate() error {
	vmalertmanagerlog.Info("validate create", "name", r.Name)
	if mustSkipValidation(r) {
		return nil
	}
	if err := r.sanityCheck(); err != nil {
		return err
	}
	return nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *VMAlertmanager) ValidateUpdate(old runtime.Object) error {
	vmalertmanagerlog.Info("validate update", "name", r.Name)
	if mustSkipValidation(r) {
		return nil
	}
	if err := r.sanityCheck(); err != nil {
		return err
	}
	return nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *VMAlertmanager) ValidateDelete() error {

	// no-op
	return nil
}
