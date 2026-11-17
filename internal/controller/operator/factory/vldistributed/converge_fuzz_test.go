package vldistributed_test

import (
	"testing"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/fuzzutil"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/vldistributed"
)

func FuzzConverge(f *testing.F) {
	fuzzutil.Converge(f, vldistributed.FuzzSeeds, func() *vmv1alpha1.VLDistributed { return &vmv1alpha1.VLDistributed{} }, func() fuzzutil.Reconciler { return &operator.VLDistributedReconciler{} })
}
