package v1beta1

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// log is for logging in this package.
var vmalertlog = logf.Log.WithName("vmalert-resource")

func (r *VMAlert) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// +kubebuilder:webhook:verbs=create;update,path=/validate-operator-victoriametrics-com-v1beta1-vmalert,mutating=false,failurePolicy=fail,groups=operator.victoriametrics.com,resources=vmalerts,versions=v1beta1,name=vvmalert.kb.io

var _ webhook.Validator = &VMAlert{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *VMAlert) ValidateCreate() error {
	vmalertlog.Info("validate create", "name", r.Name)

	if r.Spec.Datasource.URL == "" {
		return fmt.Errorf("spec.datasource.url cannot be empty")
	}

	// TODO(user): fill in your validation logic upon object creation.
	return nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *VMAlert) ValidateUpdate(old runtime.Object) error {
	vmalertlog.Info("validate update", "name", r.Name)

	// TODO(user): fill in your validation logic upon object update.
	return nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *VMAlert) ValidateDelete() error {

	// no-op
	return nil
}
