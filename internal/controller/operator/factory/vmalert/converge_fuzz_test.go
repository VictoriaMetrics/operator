package vmalert_test

import (
	"testing"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/fuzzutil"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/vmalert"
)

func FuzzConverge(f *testing.F) {
	fuzzutil.Converge(f, vmalert.FuzzSeeds, func() *vmv1beta1.VMAlert { return &vmv1beta1.VMAlert{} }, func() fuzzutil.Reconciler { return &operator.VMAlertReconciler{} })
}
