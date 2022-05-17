package v1beta1

import (
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// log is for logging in this package.
var vmsinglelog = logf.Log.WithName("vmsingle-resource")

func (r *VMSingle) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// +kubebuilder:webhook:verbs=create;update,admissionReviewVersions=v1,sideEffects=none,path=/validate-operator-victoriametrics-com-v1beta1-vmsingle,mutating=false,failurePolicy=fail,groups=operator.victoriametrics.com,resources=vmsingles,versions=v1beta1,name=vvmsingle.kb.io

var _ webhook.Validator = &VMSingle{}

func (r *VMSingle) sanityCheck() error {
	// todo add some checks.
	return nil
}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *VMSingle) ValidateCreate() error {
	vmsinglelog.Info("validate create", "name", r.Name)
	if mustSkipValidation(r) {
		return nil
	}
	if err := r.sanityCheck(); err != nil {
		return err
	}
	return nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *VMSingle) ValidateUpdate(old runtime.Object) error {
	vmsinglelog.Info("validate update", "name", r.Name)
	if mustSkipValidation(r) {
		return nil
	}
	if err := r.sanityCheck(); err != nil {
		return err
	}
	// todo check for negative storage resize.
	return nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *VMSingle) ValidateDelete() error {
	// no-op
	return nil
}
