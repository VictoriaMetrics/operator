package vlagent

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
		`{"spec":{"remoteWrite":[{"url":"http://vl:9428"}]}}`,
		`{"spec":{"remoteWrite":[{"url":"http://vl:9428"}],"k8sCollector":{"enabled":true},"serviceAccountName":"sa","vpa":{}}}`,
		`{"spec":{"remoteWrite":[{"url":"http://vl:9428"}],"replicaCount":2,"podDisruptionBudget":{"minAvailable":1}},"status":{"lastAppliedSpec":{"remoteWrite":[{"url":"http://old:9428"}],"replicaCount":1}}}`,
		`{"spec":{"paused":true}}`,
	}, func() *vmv1.VLAgent { return &vmv1.VLAgent{} }, func(ctx context.Context, rclient client.Client, cr *vmv1.VLAgent) error {
		return CreateOrUpdate(ctx, cr, rclient)
	})
}
