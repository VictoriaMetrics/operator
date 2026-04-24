package build

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
)

// PodTemplateAddCommonParams populates common pod-level fields on a PodTemplateSpec
func PodTemplateAddCommonParams(dst *corev1.PodTemplateSpec, params *vmv1beta1.CommonAppsParams) {
	dst.Spec.Affinity = params.Affinity
	dst.Spec.Tolerations = params.Tolerations
	dst.Spec.SchedulerName = params.SchedulerName
	dst.Spec.RuntimeClassName = params.RuntimeClassName
	dst.Spec.HostAliases = params.HostAliases
	if len(params.HostAliasesUnderScore) > 0 {
		dst.Spec.HostAliases = params.HostAliasesUnderScore
	}
	dst.Spec.PriorityClassName = params.PriorityClassName
	dst.Spec.HostNetwork = params.HostNetwork
	dst.Spec.DNSPolicy = params.DNSPolicy
	dst.Spec.DNSConfig = params.DNSConfig
	dst.Spec.NodeSelector = params.NodeSelector
	dst.Spec.SecurityContext = addStrictSecuritySettingsToPod(params)
	dst.Spec.TerminationGracePeriodSeconds = params.TerminationGracePeriodSeconds
	dst.Spec.TopologySpreadConstraints = params.TopologySpreadConstraints
	dst.Spec.ImagePullSecrets = params.ImagePullSecrets
	dst.Spec.ReadinessGates = params.ReadinessGates
	if params.DisableAutomountServiceAccountToken {
		dst.Spec.AutomountServiceAccountToken = ptr.To(false)
	}

	cfg := config.MustGetBaseConfig()
	if len(cfg.CommonAnnotations) > 0 {
		dst.Annotations = labels.Merge(cfg.CommonAnnotations, dst.Annotations)
	}
	if len(cfg.CommonLabels) > 0 {
		dst.Labels = labels.Merge(cfg.CommonLabels, dst.Labels)
	}
}
