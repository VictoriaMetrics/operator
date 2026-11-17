package vmagent_test

import (
	"testing"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/fuzzutil"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/vmagent"
)

func FuzzConverge(f *testing.F) {
	fuzzutil.Converge(f, vmagent.FuzzSeeds, func() *vmv1beta1.VMAgent { return &vmv1beta1.VMAgent{} }, func() fuzzutil.Reconciler { return &operator.VMAgentReconciler{} })
}
