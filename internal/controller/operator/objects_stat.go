package operator

import (
	"fmt"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	initCollector sync.Once
	collector     *objectCollector
)

type objectCollector struct {
	mu                  sync.Mutex
	objectsByController map[string]sets.Set[string]
}

func (oc *objectCollector) register(name, ns, controller string) {
	oc.mu.Lock()
	defer oc.mu.Unlock()
	oc.objectsByController[controller].Insert(ns + "/" + name)
}

func (oc *objectCollector) deRegister(name, ns, controller string) {
	oc.mu.Lock()
	defer oc.mu.Unlock()
	oc.objectsByController[controller].Delete(ns + "/" + name)
}

func (oc *objectCollector) countByController(controller string) float64 {
	oc.mu.Lock()
	defer oc.mu.Unlock()
	objects, ok := oc.objectsByController[controller]
	if !ok {
		panic(fmt.Sprintf("BUG, controller: %s is not registered", controller))
	}
	return float64(objects.Len())
}

func newCollector() *objectCollector {
	oc := &objectCollector{
		objectsByController: map[string]sets.Set[string]{},
	}
	registeredObjects := []string{
		"vmagent", "vmalert", "vmsingle", "vmcluster", "vmalertmanager", "vmauth", "vlogs", "vlsingle",
		"vlcluster", "vmalertmanagerconfig", "vmrule", "vmuser", "vmservicescrape", "vmstaticscrape",
		"vmnodescrape", "vmpodscrape", "vmprobescrape", "vmscrapeconfig", "vmanomaly", "vlagent",
		"vtsingle", "vtcluster", "vmdistributed", "podmonitor", "prometheusrule", "servicemonitor",
		"alertmanagerconfig", "probe", "scrapeconfig", "vmanomalyconfig",
	}
	for _, controller := range registeredObjects {
		oc.objectsByController[controller] = sets.New[string]()
	}
	registry := metrics.Registry
	instrumentMetric := func(controller string) prometheus.GaugeFunc {
		g := prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name:        "operator_controller_objects_count",
			Help:        "Number of exist CR objects by controller",
			ConstLabels: map[string]string{"controller": controller},
		},
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

// registerObject registers given CR object name with namespace for controller
func registerObjectByCollector(name, ns, controller string) {
	initCollector.Do(func() {
		collector = newCollector()
	})
	collector.register(name, ns, controller)
}

// deregisterObject removes from cache given CR object name with namespace for controller
func deregisterObjectByCollector(name, ns, controller string) {
	initCollector.Do(func() {
		collector = newCollector()
	})
	collector.deRegister(name, ns, controller)
}

// RegisterObjectStat registers or deregisters object at metrics
func RegisterObjectStat(obj client.Object, controller string) {
	if obj.GetDeletionTimestamp().IsZero() {
		registerObjectByCollector(obj.GetName(), obj.GetNamespace(), controller)
		return
	}
	deregisterObjectByCollector(obj.GetName(), obj.GetNamespace(), controller)
}
