package vmauth_test

import (
	"testing"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/fuzzutil"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/vmauth"
)

func FuzzConverge(f *testing.F) {
	fuzzutil.Converge(f, vmauth.FuzzSeeds, func() *vmv1beta1.VMAuth { return &vmv1beta1.VMAuth{} }, func() fuzzutil.Reconciler { return &operator.VMAuthReconciler{} })
}
