package factory

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	badConfigsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "operator_controller_bad_objects_count",
		Help: "Number of incorrect objects by controller"}, []string{"controller"})
)

func init() {
	metrics.Registry.MustRegister(badConfigsTotal)
}
