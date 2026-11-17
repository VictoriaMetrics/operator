package vlcluster_test

import (
	"testing"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/fuzzutil"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/vlcluster"
)

func FuzzConverge(f *testing.F) {
	fuzzutil.Converge(f, vlcluster.FuzzSeeds, func() *vmv1.VLCluster { return &vmv1.VLCluster{} }, func() fuzzutil.Reconciler { return &operator.VLClusterReconciler{} })
}
