package vmalert

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
		`{"spec":{"datasource":{"url":"http://datasource"},"notifier":{"url":"http://notifier"}}}`,
		`{"spec":{"datasource":{"url":"http://datasource"},"notifiers":[{"url":"http://n1"},{"url":"http://n2"}],"replicaCount":2,"serviceAccountName":"sa","podDisruptionBudget":{"minAvailable":1},"vpa":{}}}`,
		`{"spec":{"datasource":{"url":"http://datasource"},"notifier":{"selector":{}},"selectAllByDefault":true}}`,
		`{"spec":{"datasource":{"url":"http://datasource"},"notifier":{"url":"http://notifier"},"replicaCount":2},"status":{"lastAppliedSpec":{"datasource":{"url":"http://old"},"notifier":{"url":"http://notifier"},"replicaCount":1}}}`,
		`{"spec":{"paused":true}}`,
	}, func() *vmv1beta1.VMAlert { return &vmv1beta1.VMAlert{} }, func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAlert) error {
		return CreateOrUpdate(ctx, cr, rclient, nil)
	})
}
