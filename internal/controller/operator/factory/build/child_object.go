package build

import (
	"context"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

var badObjectsTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "operator_bad_objects_total",
		Help: "Number of incorrect child objects by CRD type and namespace",
	},
	[]string{"crd", "object_namespace"},
)

var badObjectsCountDeprecated = map[string]prometheus.Counter{
	"vmalertmanagerconfig": prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "operator_alertmanager_bad_objects_count",
			Help: "Number of child CRDs with bad or incomplete configurations",
			ConstLabels: prometheus.Labels{
				"crd": "vmalertmanager_config",
			},
		},
	),
	"vmrule": prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "operator_vmalert_bad_objects_count",
			Help: "Number of incorrect objects by controller",
			ConstLabels: prometheus.Labels{
				"controller": "vmrules",
			},
		},
	),
}

func init() {
	metrics.Registry.MustRegister(badObjectsTotal)
	for _, m := range badObjectsCountDeprecated {
		metrics.Registry.MustRegister(m)
	}
}

type scrapeObjectWithStatus interface {
	client.Object
	GetStatusMetadata() *vmv1beta1.StatusMetadata
	AsKey(bool) string
}

// ChildObjects represents collected by parent CR child objects and provides interface for validating them using externally defined callbacks
// and updating metrics, that represent current status.
type ChildObjects[T scrapeObjectWithStatus] struct {
	valid             []T
	broken            []T
	brokenByNamespace map[string]int
	nsn               []string
	zero              T
	name              string
}

// NewChildObjects creates new ChildObject instance
func NewChildObjects[T scrapeObjectWithStatus](name string, valid []T, nsn []string) *ChildObjects[T] {
	OrderByKeys(valid, nsn)
	return &ChildObjects[T]{
		valid: valid,
		name:  name,
		nsn:   nsn,
	}
}

// Get returns first valid or broken object with same name and namespace as of t, returns nil if not found
func (co *ChildObjects[T]) Get(t T) T {
	for _, o := range co.valid {
		if o.GetName() == t.GetName() && o.GetNamespace() == t.GetNamespace() {
			return o
		}
	}
	for _, o := range co.broken {
		if o.GetName() == t.GetName() && o.GetNamespace() == t.GetNamespace() {
			return o
		}
	}
	return co.zero
}

// Broken returns slice of broken objects
func (co *ChildObjects[T]) Broken() []T {
	return co.broken
}

// All returns slice of valid and broken objects
func (co *ChildObjects[T]) All() []T {
	return append(co.valid, co.broken...)
}

// UpdateMetrics updates internal metrics depending on current ChildObjects instance state
func (co *ChildObjects[T]) UpdateMetrics(ctx context.Context) {
	logger.SelectedObjects(ctx, co.name, len(co.nsn), len(co.broken), co.nsn)
	m, hasDeprecatedMetric := badObjectsCountDeprecated[co.name]
	var total int
	for ns, cnt := range co.brokenByNamespace {
		if hasDeprecatedMetric {
			total += cnt
		}
		badObjectsTotal.WithLabelValues(co.name, ns).Add(float64(cnt))
	}
	if hasDeprecatedMetric {
		m.Add(float64(total))
	}
}

// ForEachCollectSkipInvalid iterates over all valid objects applying validate to each of them
// object is added to broken slice of validation failed, but it doesn't break a loop
func (co *ChildObjects[T]) ForEachCollectSkipInvalid(validate func(s T) error) {
	if err := co.forEachCollectSkipOn(validate, func(_ error) bool { return true }); err != nil {
		panic(fmt.Sprintf("BUG: unexpected error: %s", err))
	}
}

// ForEachCollectSkipNotFound iterates over all valid objects applying validate to each of them
// object is added to broken slice of validation failed and breaks a loop if error type is not NotFound
func (co *ChildObjects[T]) ForEachCollectSkipNotFound(validate func(s T) error) error {
	return co.forEachCollectSkipOn(validate, IsNotFound)
}

func (co *ChildObjects[T]) setErrorStatus(v T, err error) {
	st := v.GetStatusMetadata()
	st.CurrentSyncError = err.Error()
	co.broken = append(co.broken, v)
	if co.brokenByNamespace == nil {
		co.brokenByNamespace = make(map[string]int)
	}
	co.brokenByNamespace[v.GetNamespace()]++
}

func (co *ChildObjects[T]) forEachCollectSkipOn(apply func(s T) error, shouldIgnoreError func(error) bool) error {
	valid := co.valid[:0]
	uniqIds := make(map[string]int)
	for _, o := range co.valid {
		if err := apply(o); err != nil {
			if !shouldIgnoreError(err) {
				return err
			}
			co.setErrorStatus(o, err)
			continue
		}
		key := o.AsKey(false)
		idx, ok := uniqIds[key]
		if !ok {
			idx = len(valid)
			valid = append(valid, o)
			uniqIds[key] = idx
			continue
		}
		item := valid[idx]
		if item.GetCreationTimestamp().After(o.GetCreationTimestamp().Time) {
			item, o = o, item
			valid[idx] = o
		}
		err := fmt.Errorf("%s=%s has duplicate id with %s=%s", co.name, o.AsKey(true), co.name, item.AsKey(true))
		co.setErrorStatus(o, err)
	}
	co.valid = valid
	return nil
}
