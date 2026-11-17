package vmsingle_test

import (
	"testing"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/fuzzutil"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/vmsingle"
)

func FuzzConverge(f *testing.F) {
	fuzzutil.Converge(f, vmsingle.FuzzSeeds, func() *vmv1beta1.VMSingle { return &vmv1beta1.VMSingle{} }, func() fuzzutil.Reconciler { return &operator.VMSingleReconciler{} })
}
