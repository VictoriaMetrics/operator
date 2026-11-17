package vmalertmanager_test

import (
	"testing"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/fuzzutil"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/vmalertmanager"
)

func FuzzConverge(f *testing.F) {
	fuzzutil.Converge(f, vmalertmanager.FuzzSeeds, func() *vmv1beta1.VMAlertmanager { return &vmv1beta1.VMAlertmanager{} }, func() fuzzutil.Reconciler { return &operator.VMAlertmanagerReconciler{} })
}
