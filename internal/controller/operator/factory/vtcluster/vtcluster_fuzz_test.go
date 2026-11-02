package vtcluster

import (
	"context"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/fuzzutil"
)

func FuzzCreateOrUpdate(f *testing.F) {
	fuzzutil.CreateOrUpdate(f, []string{
		`{}`,
		`{"spec":{"insert":{"replicaCount":1},"select":{"replicaCount":1},"storage":{"replicaCount":1}}}`,
		`{"spec":{"insert":{"replicaCount":1,"hpa":{"minReplicas":1,"maxReplicas":3},"vpa":{}},"select":{"replicaCount":1,"podDisruptionBudget":{"minAvailable":1}},"storage":{"replicaCount":2,"storage":{"volumeClaimTemplate":{"spec":{"resources":{"requests":{"storage":"1Gi"}}}}}},"requestsLoadBalancer":{"enabled":true}}}`,
		`{"spec":{"storage":{"replicaCount":2}},"status":{"lastAppliedSpec":{"storage":{"replicaCount":1},"select":{"replicaCount":1}}}}`,
		`{"spec":{"paused":true}}`,
	}, func() *vmv1.VTCluster { return &vmv1.VTCluster{} }, func(ctx context.Context, rclient client.Client, cr *vmv1.VTCluster) error {
		return CreateOrUpdate(ctx, rclient, cr)
	})
}
