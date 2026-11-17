package vtsingle

import (
	"testing"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/fuzzutil"
)

// FuzzSeeds are VTSingle specs shared by fuzz targets.
var FuzzSeeds = []string{
	`{}`,
	`{"spec":{"replicaCount":1}}`,
	`{"spec":{"storage":{"resources":{"requests":{"storage":"1Gi"}}},"retentionPeriod":"1w","extraArgs":{"a":"b"}}}`,
	`{"spec":{"vpa":{"updatePolicy":{"updateMode":"Auto"}},"networkPolicy":{}}}`,
	`{"spec":{"serviceAccountName":"sa","podDisruptionBudget":{"minAvailable":1}}}`,
	`{"spec":{"paused":true}}`,
	`{"spec":{"replicaCount":2},"status":{"lastAppliedSpec":{"replicaCount":1,"storage":{"resources":{"requests":{"storage":"1Gi"}}}}}}`,
}

func FuzzCreateOrUpdate(f *testing.F) {
	fuzzutil.CreateOrUpdate(f, FuzzSeeds, func() *vmv1.VTSingle { return &vmv1.VTSingle{} }, CreateOrUpdate)
}
