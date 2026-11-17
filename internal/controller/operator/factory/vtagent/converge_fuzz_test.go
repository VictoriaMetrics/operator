package vtagent_test

import (
	"testing"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/fuzzutil"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/vtagent"
)

func FuzzConverge(f *testing.F) {
	fuzzutil.Converge(f, vtagent.FuzzSeeds, func() *vmv1.VTAgent { return &vmv1.VTAgent{} }, func() fuzzutil.Reconciler { return &operator.VTAgentReconciler{} })
}
