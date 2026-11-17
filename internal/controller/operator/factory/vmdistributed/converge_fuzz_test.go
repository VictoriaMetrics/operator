package vmdistributed_test

import (
	"testing"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/fuzzutil"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/vmdistributed"
)

func FuzzConverge(f *testing.F) {
	fuzzutil.Converge(f, vmdistributed.FuzzSeeds, func() *vmv1alpha1.VMDistributed { return &vmv1alpha1.VMDistributed{} }, func() fuzzutil.Reconciler { return &operator.VMDistributedReconciler{} })
}
