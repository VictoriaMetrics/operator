package vlcluster

import (
	"testing"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/fuzzutil"
)

func FuzzCreateOrUpdate(f *testing.F) {
	fuzzutil.CreateOrUpdate(f, []string{
		`{}`,
		`{"spec":{"vlinsert":{"replicaCount":1},"vlselect":{"replicaCount":1},"vlstorage":{"replicaCount":1}}}`,
		`{"spec":{"vlstorage":{"replicaCount":1,"podDisruptionBudget":{"minAvailable":1},"hpa":{"minReplicas":1,"maxReplicas":3},"vpa":{"updatePolicy":{"updateMode":"Auto"},"resourcePolicy":{"containerPolicies":[{"containerName":"*"}]}},"networkPolicy":{"ingress":[{}]},"serviceSpec":{"metadata":{"name":"extra"},"spec":{"type":"NodePort"}}},"vlselect":{"replicaCount":1,"podDisruptionBudget":{"minAvailable":1},"hpa":{"minReplicas":1,"maxReplicas":3},"vpa":{"updatePolicy":{"updateMode":"Auto"},"resourcePolicy":{"containerPolicies":[{"containerName":"*"}]}},"networkPolicy":{"ingress":[{}]},"serviceSpec":{"metadata":{"name":"extra"},"spec":{"type":"NodePort"}},"storage":{"volumeClaimTemplate":{"spec":{"resources":{"requests":{"storage":"1Gi"}}}}}},"vlinsert":{"replicaCount":1,"podDisruptionBudget":{"minAvailable":1},"hpa":{"minReplicas":1,"maxReplicas":3},"vpa":{"updatePolicy":{"updateMode":"Auto"},"resourcePolicy":{"containerPolicies":[{"containerName":"*"}]}},"networkPolicy":{"ingress":[{}]},"serviceSpec":{"metadata":{"name":"extra"},"spec":{"type":"NodePort"}}},"requestsLoadBalancer":{"enabled":true,"spec":{"podDisruptionBudget":{"minAvailable":1},"hpa":{"minReplicas":1,"maxReplicas":2},"networkPolicy":{"ingress":[{}]},"serviceSpec":{"metadata":{"name":"lb"},"spec":{}}}}}}`,
		`{"spec":{"vlstorage":{"replicaCount":1}},"status":{"lastAppliedSpec":{"vlstorage":{"replicaCount":1,"podDisruptionBudget":{"minAvailable":1},"hpa":{"minReplicas":1,"maxReplicas":3},"vpa":{"updatePolicy":{"updateMode":"Auto"},"resourcePolicy":{"containerPolicies":[{"containerName":"*"}]}},"networkPolicy":{"ingress":[{}]},"serviceSpec":{"metadata":{"name":"extra"},"spec":{"type":"NodePort"}}},"vlselect":{"replicaCount":1,"podDisruptionBudget":{"minAvailable":1},"hpa":{"minReplicas":1,"maxReplicas":3},"vpa":{"updatePolicy":{"updateMode":"Auto"},"resourcePolicy":{"containerPolicies":[{"containerName":"*"}]}},"networkPolicy":{"ingress":[{}]},"serviceSpec":{"metadata":{"name":"extra"},"spec":{"type":"NodePort"}},"storage":{"volumeClaimTemplate":{"spec":{"resources":{"requests":{"storage":"1Gi"}}}}}},"vlinsert":{"replicaCount":1,"podDisruptionBudget":{"minAvailable":1},"hpa":{"minReplicas":1,"maxReplicas":3},"vpa":{"updatePolicy":{"updateMode":"Auto"},"resourcePolicy":{"containerPolicies":[{"containerName":"*"}]}},"networkPolicy":{"ingress":[{}]},"serviceSpec":{"metadata":{"name":"extra"},"spec":{"type":"NodePort"}}},"requestsLoadBalancer":{"enabled":true,"spec":{"podDisruptionBudget":{"minAvailable":1},"hpa":{"minReplicas":1,"maxReplicas":2},"networkPolicy":{"ingress":[{}]},"serviceSpec":{"metadata":{"name":"lb"},"spec":{}}}}}}}`,
		`{"spec":{"vlstorage":{"replicaCount":2}},"status":{"lastAppliedSpec":{"vlstorage":{"replicaCount":1},"vlselect":{"replicaCount":1}}}}`,
		`{"spec":{"paused":true}}`,
	}, func() *vmv1.VLCluster { return &vmv1.VLCluster{} }, CreateOrUpdate)
}
