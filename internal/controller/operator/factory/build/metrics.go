package build

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var BadObjectsTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "operator_bad_objects_count",
		Help: "Number of incorrect objects by controller",
	},
	[]string{"crd", "object_namespace"},
)

func init() {
	metrics.Registry.MustRegister(BadObjectsTotal)
}
