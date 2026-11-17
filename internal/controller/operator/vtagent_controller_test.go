package operator

import (
	"testing"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestVTAgentReconcileNotFound(t *testing.T) {
	c := k8stools.GetTestClientWithObjects(nil)
	r := &VTAgentReconciler{}
	r.Init("VTAgent", c, logr.Discard(), c.Scheme(), config.MustGetBaseConfig())

	// deleted CR must not be retried
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "gone", Namespace: "default"}}
	if _, err := r.Reconcile(t.Context(), req); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
}
