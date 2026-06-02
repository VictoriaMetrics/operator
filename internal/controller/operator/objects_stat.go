package operator

import (
	"fmt"
	"strings"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

var (
	initCollector sync.Once
	collector     *objectCollector
)

var objectStatusDesc = prometheus.NewDesc(
	"operator_object_status",
	"Current update status of operator-managed objects (1=active for that status, 0=inactive)",
	[]string{"controller", "namespace", "name", "status"},
	nil,
)

var allUpdateStatuses = []vmv1beta1.UpdateStatus{
	vmv1beta1.UpdateStatusOperational,
	vmv1beta1.UpdateStatusFailed,
	vmv1beta1.UpdateStatusExpanding,
	vmv1beta1.UpdateStatusPaused,
}

type objectCollector struct {
	mu                  sync.Mutex
	objectsByController map[string]sets.Set[string]
	statusByObject      map[string]vmv1beta1.UpdateStatus
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
	delete(oc.statusByObject, controller+"/"+ns+"/"+name)
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

func (oc *objectCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- objectStatusDesc
}

func (oc *objectCollector) Collect(ch chan<- prometheus.Metric) {
	oc.mu.Lock()
	defer oc.mu.Unlock()
	for key, current := range oc.statusByObject {
		parts := strings.SplitN(key, "/", 3)
		if len(parts) != 3 {
			continue
		}
		controller, ns, name := parts[0], parts[1], parts[2]
		for _, s := range allUpdateStatuses {
			val := 0.0
			if current == s {
				val = 1.0
			}
			ch <- prometheus.MustNewConstMetric(objectStatusDesc, prometheus.GaugeValue, val,
				controller, ns, name, string(s))
		}
	}
}

func newCollector() *objectCollector {
	oc := &objectCollector{
		objectsByController: map[string]sets.Set[string]{},
		statusByObject:      map[string]vmv1beta1.UpdateStatus{},
	}
	registeredObjects := []string{
		"vmagent", "vmalert", "vmsingle", "vmcluster", "vmalertmanager", "vmauth", "vlogs", "vlsingle",
		"vlcluster", "vmalertmanagerconfig", "vmrule", "vmuser", "vmservicescrape", "vmstaticscrape",
		"vmnodescrape", "vmpodscrape", "vmprobe", "vmscrapeconfig", "vmanomaly", "vlagent",
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
	registry.MustRegister(oc)
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
	if controller == "" {
		return
	}
	controller = strings.ToLower(controller)
	if obj.GetDeletionTimestamp().IsZero() {
		registerObjectByCollector(obj.GetName(), obj.GetNamespace(), controller)
		return
	}
	deregisterObjectByCollector(obj.GetName(), obj.GetNamespace(), controller)
}

// RegisterObjectStatus records the current UpdateStatus for a primary workload object.
func RegisterObjectStatus(obj client.Object, controller string, status vmv1beta1.UpdateStatus) {
	initCollector.Do(func() {
		collector = newCollector()
	})
	key := controller + "/" + obj.GetNamespace() + "/" + obj.GetName()
	collector.mu.Lock()
	collector.statusByObject[key] = status
	collector.mu.Unlock()
}
