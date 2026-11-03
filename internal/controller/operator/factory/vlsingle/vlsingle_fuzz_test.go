package vlsingle

import (
	"testing"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/fuzzutil"
)

func FuzzCreateOrUpdate(f *testing.F) {
	fuzzutil.CreateOrUpdate(f, []string{
		`{}`,
		`{"spec":{"replicaCount":1}}`,
		`{"spec":{"storage":{"resources":{"requests":{"storage":"1Gi"}}},"retentionPeriod":"1w","extraArgs":{"a":"b"}}}`,
		`{"spec":{"vpa":{"updatePolicy":{"updateMode":"Auto"}},"networkPolicy":{},"podDisruptionBudget":{"minAvailable":1}}}`,
		`{"spec":{"replicaCount":2},"status":{"lastAppliedSpec":{"replicaCount":1,"storage":{"resources":{"requests":{"storage":"1Gi"}}}}}}`,
		`{"spec":{"paused":true}}`,
	}, func() *vmv1.VLSingle { return &vmv1.VLSingle{} }, CreateOrUpdate)
}
