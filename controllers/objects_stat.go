package controllers

import (
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	"sync"
)

var (
	initCollector sync.Once
	collector     *objectCollector
)

type objectCollector struct {
	mu                  sync.Mutex
	objectsByController map[string]map[string]struct{}
}

func (oc *objectCollector) register(name, ns, controller string) {
	oc.mu.Lock()
	defer oc.mu.Unlock()
	oc.objectsByController[controller][ns+"/"+name] = struct{}{}
}

func (oc *objectCollector) deRegister(name, ns, controller string) {
	oc.mu.Lock()
	defer oc.mu.Unlock()
	delete(oc.objectsByController[controller], ns+"/"+name)
}

func (oc *objectCollector) countByController(controller string) float64 {
	oc.mu.Lock()
	defer oc.mu.Unlock()
	objects, ok := oc.objectsByController[controller]
	if !ok {
		panic(fmt.Sprintf("BUG, controller: %s is not registered", controller))
	}
	return float64(len(objects))
}

func newCollector() *objectCollector {
	oc := &objectCollector{
		objectsByController: map[string]map[string]struct{}{},
	}
	registeredObjects := []string{"vmagent", "vmalert", "vmsingle", "vmcluster", "vmalertmanager", "vmauth",
		"vmalertmanagerconfig", "vmrule", "vmuser", "vmservicescrape", "vmstaticscrape", "vmnodescrape", "vmpodscrape", "vmprobescrape"}
	for _, controller := range registeredObjects {
		oc.objectsByController[controller] = map[string]struct{}{}
	}
	registry := metrics.Registry
	instrumentMetric := func(controller string) prometheus.GaugeFunc {
		g := prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name:        "operator_controller_objects_count",
			Help:        "Number of exist CR objects by controller",
			ConstLabels: map[string]string{"controller": controller}},
			func() float64 {
				return oc.countByController(controller)
			})
		return g
	}

	for _, controller := range registeredObjects {
		registry.MustRegister(instrumentMetric(controller))
	}
	return oc
}

// RegisterObject registers given CR object name with namespace for controller
func RegisterObject(name, ns, controller string) {
	initCollector.Do(func() {
		collector = newCollector()
	})
	collector.register(name, ns, controller)
}

// DeregisterObject removes from cache given CR object name with namespace for controller
func DeregisterObject(name, ns, controller string) {
	initCollector.Do(func() {
		collector = newCollector()
	})
	collector.deRegister(name, ns, controller)
}
