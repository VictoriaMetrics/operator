package vtcluster_test

import (
	"testing"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/fuzzutil"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/vtcluster"
)

func FuzzConverge(f *testing.F) {
	fuzzutil.Converge(f, vtcluster.FuzzSeeds, func() *vmv1.VTCluster { return &vmv1.VTCluster{} }, func() fuzzutil.Reconciler { return &operator.VTClusterReconciler{} })
}
