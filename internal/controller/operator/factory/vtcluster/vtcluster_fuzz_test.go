package vtcluster

import (
	"testing"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/fuzzutil"
)

// FuzzSeeds are VTCluster specs shared by fuzz targets.
var FuzzSeeds = []string{
	`{}`,
	`{"spec":{"insert":{"replicaCount":1},"select":{"replicaCount":1},"storage":{"replicaCount":1}}}`,
	`{"spec":{"storage":{"replicaCount":1,"podDisruptionBudget":{"minAvailable":1},"hpa":{"minReplicas":1,"maxReplicas":3},"vpa":{"updatePolicy":{"updateMode":"Auto"},"resourcePolicy":{"containerPolicies":[{"containerName":"*"}]}},"networkPolicy":{"ingress":[{}]},"serviceSpec":{"metadata":{"name":"extra-1"},"spec":{"type":"NodePort"}},"storage":{"volumeClaimTemplate":{"spec":{"resources":{"requests":{"storage":"1Gi"}}}}}},"select":{"replicaCount":1,"podDisruptionBudget":{"minAvailable":1},"hpa":{"minReplicas":1,"maxReplicas":3},"vpa":{"updatePolicy":{"updateMode":"Auto"},"resourcePolicy":{"containerPolicies":[{"containerName":"*"}]}},"networkPolicy":{"ingress":[{}]},"serviceSpec":{"metadata":{"name":"extra-2"},"spec":{"type":"NodePort"}}},"insert":{"replicaCount":1,"podDisruptionBudget":{"minAvailable":1},"hpa":{"minReplicas":1,"maxReplicas":3},"vpa":{"updatePolicy":{"updateMode":"Auto"},"resourcePolicy":{"containerPolicies":[{"containerName":"*"}]}},"networkPolicy":{"ingress":[{}]},"serviceSpec":{"metadata":{"name":"extra-3"},"spec":{"type":"NodePort"}}},"requestsLoadBalancer":{"enabled":true,"spec":{"podDisruptionBudget":{"minAvailable":1},"hpa":{"minReplicas":1,"maxReplicas":2},"networkPolicy":{"ingress":[{}]},"serviceSpec":{"metadata":{"name":"lb"},"spec":{}}}}}}`,
	`{"spec":{"storage":{"replicaCount":1}},"status":{"lastAppliedSpec":{"storage":{"replicaCount":1,"podDisruptionBudget":{"minAvailable":1},"hpa":{"minReplicas":1,"maxReplicas":3},"vpa":{"updatePolicy":{"updateMode":"Auto"},"resourcePolicy":{"containerPolicies":[{"containerName":"*"}]}},"networkPolicy":{"ingress":[{}]},"serviceSpec":{"metadata":{"name":"extra-1"},"spec":{"type":"NodePort"}},"storage":{"volumeClaimTemplate":{"spec":{"resources":{"requests":{"storage":"1Gi"}}}}}},"select":{"replicaCount":1,"podDisruptionBudget":{"minAvailable":1},"hpa":{"minReplicas":1,"maxReplicas":3},"vpa":{"updatePolicy":{"updateMode":"Auto"},"resourcePolicy":{"containerPolicies":[{"containerName":"*"}]}},"networkPolicy":{"ingress":[{}]},"serviceSpec":{"metadata":{"name":"extra-2"},"spec":{"type":"NodePort"}}},"insert":{"replicaCount":1,"podDisruptionBudget":{"minAvailable":1},"hpa":{"minReplicas":1,"maxReplicas":3},"vpa":{"updatePolicy":{"updateMode":"Auto"},"resourcePolicy":{"containerPolicies":[{"containerName":"*"}]}},"networkPolicy":{"ingress":[{}]},"serviceSpec":{"metadata":{"name":"extra-3"},"spec":{"type":"NodePort"}}},"requestsLoadBalancer":{"enabled":true,"spec":{"podDisruptionBudget":{"minAvailable":1},"hpa":{"minReplicas":1,"maxReplicas":2},"networkPolicy":{"ingress":[{}]},"serviceSpec":{"metadata":{"name":"lb"},"spec":{}}}}}}}`,
	`{"spec":{"storage":{"replicaCount":2}},"status":{"lastAppliedSpec":{"storage":{"replicaCount":1},"select":{"replicaCount":1}}}}`,
	`{"spec":{"paused":true}}`,
}

func FuzzCreateOrUpdate(f *testing.F) {
	fuzzutil.CreateOrUpdate(f, FuzzSeeds, func() *vmv1.VTCluster { return &vmv1.VTCluster{} }, CreateOrUpdate)
}
