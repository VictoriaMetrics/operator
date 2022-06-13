package v1beta1

import (
	"fmt"
	"github.com/VictoriaMetrics/VictoriaMetrics/app/vmalert/config"
	"gopkg.in/yaml.v2"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// log is for logging in this package.
var vmrulelog = logf.Log.WithName("vmrule-resource")

func (r *VMRule) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// +kubebuilder:webhook:verbs=create;update,admissionReviewVersions=v1,sideEffects=none,path=/validate-operator-victoriametrics-com-v1beta1-vmrule,mutating=false,failurePolicy=fail,groups=operator.victoriametrics.com,resources=vmrules,versions=v1beta1,name=vvmrule.kb.io
var _ webhook.Validator = &VMRule{}

func (r *VMRule) sanityCheck() error {

	uniqNames := make(map[string]struct{})
	for i := range r.Spec.Groups {
		group := &r.Spec.Groups[i]
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
		if err := yaml.Unmarshal(groupBytes, &vmalertGroup); err != nil {
			return fmt.Errorf("cannot parse vmalert group %s, err: %w, r: \n%s", errContext, err, string(groupBytes))
		}
		if err := vmalertGroup.Validate(true, true); err != nil {
			return fmt.Errorf("validation failed for %s err: %w", errContext, err)
		}
	}
	vmrulelog.Info("successfully validated rule", "name", r.Name)
	return nil
}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *VMRule) ValidateCreate() error {
	vmrulelog.Info("validate create", "name", r.Name)
	// skip validation, if object has annotation.
	if mustSkipValidation(r) {
		return nil
	}
	return r.sanityCheck()
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *VMRule) ValidateUpdate(old runtime.Object) error {
	vmrulelog.Info("validate update", "name", r.Name)
	if mustSkipValidation(r) {
		return nil
	}
	return r.sanityCheck()
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *VMRule) ValidateDelete() error {
	// noop
	return nil
}
