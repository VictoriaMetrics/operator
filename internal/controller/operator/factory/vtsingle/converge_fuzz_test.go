package vtsingle_test

import (
	"testing"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/fuzzutil"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/vtsingle"
)

func FuzzConverge(f *testing.F) {
	fuzzutil.Converge(f, vtsingle.FuzzSeeds, func() *vmv1.VTSingle { return &vmv1.VTSingle{} }, func() fuzzutil.Reconciler { return &operator.VTSingleReconciler{} })
}
