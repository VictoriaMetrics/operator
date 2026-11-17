package vmanomaly_test

import (
	"testing"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/fuzzutil"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/vmanomaly"
)

func FuzzConverge(f *testing.F) {
	fuzzutil.Converge(f, vmanomaly.FuzzSeeds, func() *vmv1.VMAnomaly { return &vmv1.VMAnomaly{} }, func() fuzzutil.Reconciler { return &operator.VMAnomalyReconciler{} })
}
