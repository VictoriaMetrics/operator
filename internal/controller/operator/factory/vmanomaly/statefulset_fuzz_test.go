package vmanomaly

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
		`{"spec":{"configRawYaml":"models:\n  M1:\n    class: zscore\n    queries: [q1]\n    schedulers: [S1]\nreader:\n  queries:\n    q1:\n      expr: sum(up)\nschedulers:\n  S1:\n    class: periodic\n    infer_every: 1m\n","reader":{"datasourceURL":"http://reader","samplingPeriod":"1m"},"writer":{"datasourceURL":"http://writer"}}}`,
		`{"spec":{"reader":{"datasourceURL":"http://reader","samplingPeriod":"1m"},"writer":{"datasourceURL":"http://writer"},"replicaCount":2,"serviceAccountName":"sa","podDisruptionBudget":{"minAvailable":1},"vpa":{},"shardCount":2}}`,
		`{"spec":{"selectAllByDefault":true,"configSecret":{"name":"cfg","key":"c"},"reader":{"datasourceURL":"http://reader","samplingPeriod":"1m"},"writer":{"datasourceURL":"http://writer"},"monitoring":{"push":{"url":"http://push"}}}}`,
		`{"spec":{"reader":{"datasourceURL":"http://reader","samplingPeriod":"1m"},"writer":{"datasourceURL":"http://writer"},"replicaCount":2},"status":{"lastAppliedSpec":{"reader":{"datasourceURL":"http://old","samplingPeriod":"1m"},"writer":{"datasourceURL":"http://writer"},"replicaCount":1}}}`,
		`{"spec":{"paused":true}}`,
		// null model panicked config validation
		`{"spec":{"configRawYaml":"models:\n  M1:\nschedulers:\n  S1:\n    class: periodic\n    infer_every: 1m\n","reader":{"datasourceURL":"http://readnscheduler","samplingPeriod":"1m"},"writer":{"datasourceURL":"http://writer"}}}`,
	}, func() *vmv1.VMAnomaly { return &vmv1.VMAnomaly{} }, func(ctx context.Context, rclient client.Client, cr *vmv1.VMAnomaly) error {
		return CreateOrUpdate(ctx, cr, rclient)
	})
}
