package vmsingle

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
		`{"spec":{"replicaCount":1,"retentionPeriod":"1"}}`,
		`{"spec":{"retentionPeriod":"1","storage":{"resources":{"requests":{"storage":"1Gi"}}},"extraArgs":{"a":"b"},"serviceAccountName":"sa","podDisruptionBudget":{"minAvailable":1},"vpa":{},"networkPolicy":{}}}`,
		`{"spec":{"retentionPeriod":"1","ingestOnlyMode":false,"streamAggrConfig":{"rules":[{"outputs":["total"],"interval":"1m"}]}}}`,
		`{"spec":{"retentionPeriod":"1","replicaCount":2},"status":{"lastAppliedSpec":{"retentionPeriod":"1","replicaCount":1,"storage":{"resources":{"requests":{"storage":"1Gi"}}}}}}`,
		`{"spec":{"paused":true}}`,
	}, func() *vmv1beta1.VMSingle { return &vmv1beta1.VMSingle{} }, func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMSingle) error {
		return CreateOrUpdate(ctx, cr, rclient)
	})
}
