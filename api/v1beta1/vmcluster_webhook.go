package v1beta1

import (
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// log is for logging in this package.
var vmclusterlog = logf.Log.WithName("vmcluster-resource")

func (r *VMCluster) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// +kubebuilder:webhook:verbs=create;update,path=/validate-operator-victoriametrics-com-v1beta1-vmcluster,mutating=false,failurePolicy=fail,groups=operator.victoriametrics.com,resources=vmclusters,versions=v1beta1,name=vvmcluster.kb.io
var _ webhook.Validator = &VMCluster{}

func (r *VMCluster) sanityCheck() error {
	if r.Spec.VMSelect != nil {
		vms := r.Spec.VMSelect
		if vms.HPA != nil {
			if err := vms.HPA.sanityCheck(); err != nil {
				return err
			}
		}
		if vms.StorageSpec != nil {
			vmclusterlog.Info("deprecated property is defined `vmcluster.spec.vmselect.persistentVolume`, use `storage` instead.")
		}
	}
	if r.Spec.VMInsert != nil {
		vmi := r.Spec.VMInsert
		if vmi.HPA != nil {
			if err := vmi.HPA.sanityCheck(); err != nil {
				return err
			}
		}
	}
	return nil
}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *VMCluster) ValidateCreate() error {
	if mustSkipValidation(r) {
		return nil
	}
	if err := r.sanityCheck(); err != nil {
		return err
	}

	return nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *VMCluster) ValidateUpdate(old runtime.Object) error {
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
func (r *VMCluster) ValidateDelete() error {
	// no-op
	return nil
}
