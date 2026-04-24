package build

import (
	appsv1 "k8s.io/api/apps/v1"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// DeploymentAddCommonParams adds common params for all deployments
func DeploymentAddCommonParams(dst *appsv1.Deployment, params *vmv1beta1.CommonAppsParams) {
	PodTemplateAddCommonParams(&dst.Spec.Template, params)
	dst.Spec.MinReadySeconds = params.MinReadySeconds
	dst.Spec.Replicas = params.ReplicaCount
	dst.Spec.RevisionHistoryLimit = params.RevisionHistoryLimitCount
}
