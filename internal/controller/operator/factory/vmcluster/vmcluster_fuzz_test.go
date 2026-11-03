package vmcluster

import (
	"context"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/fuzzutil"
)

func FuzzCreateOrUpdate(f *testing.F) {
	fuzzutil.CreateOrUpdate(f, []string{
		`{}`,
		`{"spec":{"vmstorage":{"replicaCount":1},"vmselect":{"replicaCount":1},"vminsert":{"replicaCount":1},"retentionPeriod":"1"}}`,
		`{"spec":{"retentionPeriod":"1","license":{"key":"k"},"vmstorage":{"replicaCount":1,"podDisruptionBudget":{"minAvailable":1},"hpa":{"minReplicas":1,"maxReplicas":3},"vpa":{"updatePolicy":{"updateMode":"Auto"},"resourcePolicy":{"containerPolicies":[{"containerName":"*"}]}},"networkPolicy":{"ingress":[{}]},"serviceSpec":{"metadata":{"name":"extra"},"spec":{"type":"NodePort"}},"vmBackup":{"destination":"fs:///b"}},"vmselect":{"replicaCount":1,"podDisruptionBudget":{"minAvailable":1},"hpa":{"minReplicas":1,"maxReplicas":3},"vpa":{"updatePolicy":{"updateMode":"Auto"},"resourcePolicy":{"containerPolicies":[{"containerName":"*"}]}},"networkPolicy":{"ingress":[{}]},"serviceSpec":{"metadata":{"name":"extra"},"spec":{"type":"NodePort"}},"storage":{"volumeClaimTemplate":{"spec":{"resources":{"requests":{"storage":"1Gi"}}}}}},"vminsert":{"replicaCount":1,"podDisruptionBudget":{"minAvailable":1},"hpa":{"minReplicas":1,"maxReplicas":3},"vpa":{"updatePolicy":{"updateMode":"Auto"},"resourcePolicy":{"containerPolicies":[{"containerName":"*"}]}},"networkPolicy":{"ingress":[{}]},"serviceSpec":{"metadata":{"name":"extra"},"spec":{"type":"NodePort"}}},"requestsLoadBalancer":{"enabled":true,"spec":{"podDisruptionBudget":{"minAvailable":1},"hpa":{"minReplicas":1,"maxReplicas":2},"networkPolicy":{"ingress":[{}]},"serviceSpec":{"metadata":{"name":"lb"},"spec":{}}}}}}`,
		`{"spec":{"retentionPeriod":"1","vmstorage":{"replicaCount":1}},"status":{"lastAppliedSpec":{"retentionPeriod":"1","vmstorage":{"replicaCount":1,"podDisruptionBudget":{"minAvailable":1},"hpa":{"minReplicas":1,"maxReplicas":3},"vpa":{"updatePolicy":{"updateMode":"Auto"},"resourcePolicy":{"containerPolicies":[{"containerName":"*"}]}},"networkPolicy":{"ingress":[{}]},"serviceSpec":{"metadata":{"name":"extra"},"spec":{"type":"NodePort"}},"vmBackup":{"destination":"fs:///b"}},"vmselect":{"replicaCount":1,"podDisruptionBudget":{"minAvailable":1},"hpa":{"minReplicas":1,"maxReplicas":3},"vpa":{"updatePolicy":{"updateMode":"Auto"},"resourcePolicy":{"containerPolicies":[{"containerName":"*"}]}},"networkPolicy":{"ingress":[{}]},"serviceSpec":{"metadata":{"name":"extra"},"spec":{"type":"NodePort"}},"storage":{"volumeClaimTemplate":{"spec":{"resources":{"requests":{"storage":"1Gi"}}}}}},"vminsert":{"replicaCount":1,"podDisruptionBudget":{"minAvailable":1},"hpa":{"minReplicas":1,"maxReplicas":3},"vpa":{"updatePolicy":{"updateMode":"Auto"},"resourcePolicy":{"containerPolicies":[{"containerName":"*"}]}},"networkPolicy":{"ingress":[{}]},"serviceSpec":{"metadata":{"name":"extra"},"spec":{"type":"NodePort"}}},"requestsLoadBalancer":{"enabled":true,"spec":{"podDisruptionBudget":{"minAvailable":1},"hpa":{"minReplicas":1,"maxReplicas":2},"networkPolicy":{"ingress":[{}]},"serviceSpec":{"metadata":{"name":"lb"},"spec":{}}}}}}}`,
		`{"spec":{"retentionPeriod":"1","vmstorage":{"replicaCount":2},"vmselect":{"replicaCount":1}},"status":{"lastAppliedSpec":{"retentionPeriod":"1","vmstorage":{"replicaCount":1},"vminsert":{"replicaCount":1}}}}`,
		`{"spec":{"paused":true}}`,
	}, func() *vmv1beta1.VMCluster { return &vmv1beta1.VMCluster{} }, func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMCluster) error {
		return CreateOrUpdate(ctx, cr, rclient)
	})
}
