package vtagent

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
		`{"spec":{"remoteWrite":[{"url":"http://vt:10428"}]}}`,
		`{"spec":{"remoteWrite":[{"url":"http://vt:10428"}],"replicaCount":2,"serviceAccountName":"sa","podDisruptionBudget":{"minAvailable":1},"vpa":{}}}`,
		`{"spec":{"remoteWrite":[{"url":"http://vt:10428"}],"replicaCount":2},"status":{"lastAppliedSpec":{"remoteWrite":[{"url":"http://old:10428"}],"replicaCount":1}}}`,
		`{"spec":{"paused":true}}`,
	}, func() *vmv1.VTAgent { return &vmv1.VTAgent{} }, func(ctx context.Context, rclient client.Client, cr *vmv1.VTAgent) error {
		return CreateOrUpdate(ctx, cr, rclient)
	})
}
