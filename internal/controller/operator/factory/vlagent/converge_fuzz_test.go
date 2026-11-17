package vlagent_test

import (
	"testing"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/fuzzutil"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/vlagent"
)

func FuzzConverge(f *testing.F) {
	fuzzutil.Converge(f, vlagent.FuzzSeeds, func() *vmv1.VLAgent { return &vmv1.VLAgent{} }, func() fuzzutil.Reconciler { return &operator.VLAgentReconciler{} })
}
