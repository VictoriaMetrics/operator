package vmcluster_test

import (
	"testing"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/fuzzutil"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/vmcluster"
)

func FuzzConverge(f *testing.F) {
	fuzzutil.Converge(f, vmcluster.FuzzSeeds, func() *vmv1beta1.VMCluster { return &vmv1beta1.VMCluster{} }, func() fuzzutil.Reconciler { return &operator.VMClusterReconciler{} })
}
