package vldistributed

import (
	"context"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/fuzzutil"
)

func FuzzCreateOrUpdate(f *testing.F) {
	fuzzutil.CreateOrUpdate(f, []string{
		`{}`,
		`{"spec":{"zones":[{"name":"a"}]}}`,
		`{"spec":{"zones":[{"name":"a"},{"name":"b"}],"zoneCommon":{"updatePause":"1m","readyTimeout":"1m"}}}`,
		`{"spec":{"retain":true,"zones":[{"name":"a"},{"name":"b"}],"zoneCommon":{"updatePause":"1s"}}}`,
		`{"spec":{"zoneCommon":{"vlcluster":{"spec":{"vlinsert":{},"vlselect":{},"vlstorage":{}}}},"zones":[{"name":"a"}]}}`,
		`{"spec":{"zoneCommon":{"vlcluster":{"spec":{"vlinsert":{},"vlselect":{},"vlstorage":{}}},"updatePause":"1m","readyTimeout":"1m"},"zones":[{"name":"a"},{"name":"b"}]}}`,
		`{"spec":{"backendType":"VLSingle","zoneCommon":{"vlsingle":{"spec":{}}},"zones":[{"name":"a"},{"name":"b"}]}}`,
		`{"spec":{"paused":true}}`,
	}, func() *vmv1alpha1.VLDistributed { return &vmv1alpha1.VLDistributed{} }, func(ctx context.Context, rclient client.Client, cr *vmv1alpha1.VLDistributed) error {
		return CreateOrUpdate(ctx, cr, rclient)
	})
}
