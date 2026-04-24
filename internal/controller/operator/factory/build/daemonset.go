package build

import (
	appsv1 "k8s.io/api/apps/v1"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// DaemonSetAddCommonParams adds common params for all deployments
func DaemonSetAddCommonParams(dst *appsv1.DaemonSet, params *vmv1beta1.CommonAppsParams) {
	PodTemplateAddCommonParams(&dst.Spec.Template, params)
	dst.Spec.Template.Spec.SecurityContext = addStrictSecuritySettingsWithRootToPod(params)
	dst.Spec.MinReadySeconds = params.MinReadySeconds
	dst.Spec.RevisionHistoryLimit = params.RevisionHistoryLimitCount
}
