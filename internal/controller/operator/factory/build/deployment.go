package build

import (
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// DeploymentAddCommonParams adds common params for all deployments
func DeploymentAddCommonParams(dst *appsv1.Deployment, useStrictSecurity bool, params *vmv1beta1.CommonApplicationDeploymentParams) {
	dst.Spec.Template.Spec.Affinity = params.Affinity
	dst.Spec.Template.Spec.Tolerations = params.Tolerations
	dst.Spec.Template.Spec.SchedulerName = params.SchedulerName
	dst.Spec.Template.Spec.RuntimeClassName = params.RuntimeClassName
	dst.Spec.Template.Spec.HostAliases = params.HostAliases
	if len(params.HostAliasesUnderScore) > 0 {
		dst.Spec.Template.Spec.HostAliases = params.HostAliasesUnderScore
	}
	dst.Spec.Template.Spec.PriorityClassName = params.PriorityClassName
	dst.Spec.Template.Spec.HostNetwork = params.HostNetwork
	dst.Spec.Template.Spec.DNSPolicy = params.DNSPolicy
	dst.Spec.Template.Spec.DNSConfig = params.DNSConfig
	dst.Spec.Template.Spec.NodeSelector = params.NodeSelector
	dst.Spec.Template.Spec.SecurityContext = AddStrictSecuritySettingsToPod(params.SecurityContext, useStrictSecurity)
	dst.Spec.Template.Spec.TerminationGracePeriodSeconds = params.TerminationGracePeriodSeconds
	dst.Spec.Template.Spec.TopologySpreadConstraints = params.TopologySpreadConstraints
	dst.Spec.Template.Spec.ImagePullSecrets = params.ImagePullSecrets
	dst.Spec.Template.Spec.ReadinessGates = params.ReadinessGates
	dst.Spec.MinReadySeconds = params.MinReadySeconds
	dst.Spec.Replicas = params.ReplicaCount
	dst.Spec.RevisionHistoryLimit = params.RevisionHistoryLimitCount
	if params.DisableAutomountServiceAccountToken {
		dst.Spec.Template.Spec.AutomountServiceAccountToken = ptr.To(false)
	}
}
