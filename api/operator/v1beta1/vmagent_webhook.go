package v1beta1

import (
	"fmt"

	"github.com/VictoriaMetrics/VictoriaMetrics/lib/envtemplate"
	"github.com/VictoriaMetrics/VictoriaMetrics/lib/promrelabel"
	"gopkg.in/yaml.v2"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// log is for logging in this package.
var vmagentlog = logf.Log.WithName("vmagent-resource")

func (cr *VMAgent) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(cr).
		Complete()
}

// +kubebuilder:webhook:verbs=create;update,admissionReviewVersions=v1,sideEffects=none,path=/validate-operator-victoriametrics-com-v1beta1-vmagent,mutating=false,failurePolicy=fail,groups=operator.victoriametrics.com,resources=vmagents,versions=v1beta1,name=vvmagent.kb.io

var _ webhook.Validator = &VMAgent{}

func checkRelabelConfigs(src []RelabelConfig) error {
	for i := range src {
		currSrc := &src[i]
		if len(currSrc.UnderScoreSourceLabels) == 0 {
			currSrc.UnderScoreSourceLabels = currSrc.SourceLabels
		}
		if len(currSrc.UnderScoreTargetLabel) == 0 {
			currSrc.UnderScoreTargetLabel = currSrc.TargetLabel
		}
	}
	prc, err := yaml.Marshal(src)
	if err != nil {
		return fmt.Errorf("cannot parse relabelConfigs as yaml: %w", err)
	}
	prc, err = envtemplate.ReplaceBytes(prc)
	if err != nil {
		return fmt.Errorf("cannot replace envs: %w", err)
	}
	if _, err := promrelabel.ParseRelabelConfigsData(prc); err != nil {
		return fmt.Errorf("cannot parse relabelConfigs: %w", err)
	}
	return nil
}

func (cr *VMAgent) sanityCheck() error {
	if len(cr.Spec.RemoteWrite) == 0 {
		return fmt.Errorf("spec.remoteWrite cannot be empty array, provide at least one remoteWrite")
	}
	if cr.Spec.InlineScrapeConfig != "" {
		var inlineCfg yaml.MapSlice
		if err := yaml.Unmarshal([]byte(cr.Spec.InlineScrapeConfig), &inlineCfg); err != nil {
			return fmt.Errorf("bad cr.spec.inlineScrapeConfig it must be valid yaml, err :%w", err)
		}
	}
	if len(cr.Spec.InlineRelabelConfig) > 0 {
		if err := checkRelabelConfigs(cr.Spec.InlineRelabelConfig); err != nil {
			return err
		}
	}
	for idx, rw := range cr.Spec.RemoteWrite {
		if rw.URL == "" {
			return fmt.Errorf("remoteWrite.url cannot be empty at idx: %d", idx)
		}
		if len(rw.InlineUrlRelabelConfig) > 0 {
			if err := checkRelabelConfigs(rw.InlineUrlRelabelConfig); err != nil {
				return fmt.Errorf("bad urlRelabelingConfig at idx: %d, err: %w", idx, err)
			}
		}
	}

	return nil
}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *VMAgent) ValidateCreate() (aw admission.Warnings, err error) {
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
func (r *VMAgent) ValidateUpdate(old runtime.Object) (aw admission.Warnings, err error) {
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
func (cr *VMAgent) ValidateDelete() (aw admission.Warnings, err error) {
	// no-op
	return
}
