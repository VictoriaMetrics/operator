package vlsingle_test

import (
	"testing"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/fuzzutil"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/vlsingle"
)

func FuzzConverge(f *testing.F) {
	fuzzutil.Converge(f, vlsingle.FuzzSeeds, func() *vmv1.VLSingle { return &vmv1.VLSingle{} }, func() fuzzutil.Reconciler { return &operator.VLSingleReconciler{} })
}
