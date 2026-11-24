package v1

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func TestVMEstimatorStream_Validate(t *testing.T) {
	f := func(s VMEstimatorStream, wantErr bool) {
		t.Helper()
		err := s.Validate()
		if wantErr {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}
	}

	// empty stream uses defaults
	f(VMEstimatorStream{}, false)

	// all fields set
	f(VMEstimatorStream{
		Interval:       "15m",
		CalculateChurn: true,
		Filter:         `{job=~"api|worker",env!~"dev|staging"}`,
		GroupBy:        []string{"job", "__label__"},
		GroupLimit:     100,
		Buckets:        8,
		HLLPrecision:   18,
		HLLSparse:      new(false),
		Labels:         map[string]string{"cluster": "prod"},
	}, false)

	// incorrect interval
	f(VMEstimatorStream{Interval: "1d"}, true)
	f(VMEstimatorStream{Interval: "0s"}, true)

	// filter with or
	f(VMEstimatorStream{Filter: `{job="a" or job="b"}`}, true)

	// filter isn't a series selector
	f(VMEstimatorStream{Filter: `rate(foo[5m])`}, true)

	// filter has incorrect syntax
	f(VMEstimatorStream{Filter: `{job=`}, true)

	// too many group by labels
	f(VMEstimatorStream{GroupBy: []string{"a", "b", "c", "d", "e", "f"}}, true)

	// reserved group by labels
	f(VMEstimatorStream{GroupBy: []string{"__global__"}}, true)
	f(VMEstimatorStream{GroupBy: []string{"__group__"}}, true)
	f(VMEstimatorStream{GroupBy: []string{""}}, true)

	// __label__ is set twice
	f(VMEstimatorStream{GroupBy: []string{"__label__", "__label__"}}, true)

	// duplicated label
	f(VMEstimatorStream{GroupBy: []string{"job", "instance", "job"}}, true)

	// hll precision out of range
	f(VMEstimatorStream{HLLPrecision: 3}, true)
	f(VMEstimatorStream{HLLPrecision: 19}, true)

	// reserved static labels
	f(VMEstimatorStream{Labels: map[string]string{"interval": "1"}}, true)
	f(VMEstimatorStream{Labels: map[string]string{"group_by_keys": "1"}}, true)
	f(VMEstimatorStream{Labels: map[string]string{"by_job": "1"}}, true)
}

func TestVMEstimator_Validate(t *testing.T) {
	f := func(spec VMEstimatorSpec, wantErr bool) {
		t.Helper()
		cr := &VMEstimator{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			Spec:       spec,
		}
		err := cr.Validate()
		if wantErr {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}
	}
	streams := []VMEstimatorStream{{Interval: "5m"}}

	// streams aren't set
	f(VMEstimatorSpec{}, true)
	f(VMEstimatorSpec{
		Storage: &VMEstimatorStorage{},
		Select:  &VMEstimatorSelect{},
	}, true)

	// streams are loaded from configmap only
	f(VMEstimatorSpec{
		StreamsConfigMap: &corev1.ConfigMapKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "streams"},
			Key:                  "streams.yaml",
		},
	}, false)

	f(VMEstimatorSpec{
		Streams: streams,
		Storage: &VMEstimatorStorage{},
		Select:  &VMEstimatorSelect{},
	}, false)

	// incorrect stream
	f(VMEstimatorSpec{
		Streams: []VMEstimatorStream{{Interval: "5m"}, {Interval: "foo"}},
	}, true)

	// streams and streams configmap are mutually exclusive
	f(VMEstimatorSpec{
		Streams: streams,
		StreamsConfigMap: &corev1.ConfigMapKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "streams"},
			Key:                  "streams.yaml",
		},
	}, true)

	// incorrect streams configmap
	f(VMEstimatorSpec{
		StreamsConfigMap: &corev1.ConfigMapKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "streams"},
		},
	}, true)

	// streams producing identical time series
	f(VMEstimatorSpec{
		Streams: []VMEstimatorStream{{}, {Interval: "5m"}},
	}, true)
	f(VMEstimatorSpec{
		Streams: []VMEstimatorStream{{Interval: "5m"}, {Interval: "300s"}},
	}, true)
	f(VMEstimatorSpec{
		Streams: []VMEstimatorStream{{}, {Interval: "10m"}},
	}, false)
	f(VMEstimatorSpec{
		Streams: []VMEstimatorStream{
			{Interval: "5m", GroupBy: []string{"job"}},
			{Interval: "5m", GroupBy: []string{"job"}, GroupLimit: 100},
		},
	}, true)
	f(VMEstimatorSpec{
		Streams: []VMEstimatorStream{
			{Interval: "5m", GroupBy: []string{"job"}},
			{Interval: "5m", GroupBy: []string{"job"}, Labels: map[string]string{"team": "sre"}},
		},
	}, false)

	// storage service must stay headless for select nodes
	f(VMEstimatorSpec{
		Streams: streams,
		Storage: &VMEstimatorStorage{
			ServiceSpec: &vmv1beta1.AdditionalServiceSpec{
				UseAsDefault: true,
				Spec:         corev1.ServiceSpec{Type: corev1.ServiceTypeNodePort},
			},
		},
	}, true)
	f(VMEstimatorSpec{
		Streams: streams,
		Storage: &VMEstimatorStorage{
			ServiceSpec: &vmv1beta1.AdditionalServiceSpec{
				UseAsDefault: true,
				EmbeddedObjectMetadata: vmv1beta1.EmbeddedObjectMetadata{
					Labels: map[string]string{"team": "observability"},
				},
			},
		},
	}, false)

	// storage nodes are required, unless select uses external storage nodes
	f(VMEstimatorSpec{
		Streams: streams,
		Storage: &VMEstimatorStorage{CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: new(int32(0))}},
	}, true)
	f(VMEstimatorSpec{
		Streams: streams,
		Storage: &VMEstimatorStorage{CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: new(int32(0))}},
		Select: &VMEstimatorSelect{CommonAppsParams: vmv1beta1.CommonAppsParams{
			ExtraArgs: map[string]string{"storageNode": "http://external-storage:8490"},
		}},
	}, false)

	// incorrect select hpa
	f(VMEstimatorSpec{
		Streams: streams,
		Select: &VMEstimatorSelect{
			HPA: &vmv1beta1.EmbeddedHPA{MinReplicas: new(int32(5)), MaxReplicas: 2},
		},
	}, true)
}

func TestVMEstimator_Names(t *testing.T) {
	cr := &VMEstimator{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "monitoring"},
		Spec: VMEstimatorSpec{
			ManagedMetadata: &vmv1beta1.ManagedObjectsMetadata{
				Labels: map[string]string{"team": "observability"},
			},
		},
	}

	assert.Equal(t, "vmestimator-test", cr.PrefixedName(vmv1beta1.ClusterComponentRoot))
	assert.Equal(t, "vmestimator-storage-test", cr.PrefixedName(vmv1beta1.ClusterComponentStorage))
	assert.Equal(t, "vmestimator-select-test", cr.PrefixedName(vmv1beta1.ClusterComponentSelect))
	assert.Equal(t, "vmestimator-test", cr.GetServiceAccountName())
	assert.Equal(t, "vmestimator-test", cr.GetConfigMapName())

	assert.Equal(t, map[string]string{
		"app.kubernetes.io/name":      "vmestimator-storage",
		"app.kubernetes.io/instance":  "test",
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}, cr.SelectorLabels(vmv1beta1.ClusterComponentStorage))
	assert.Equal(t, map[string]string{
		"app.kubernetes.io/part-of":   "vmestimator",
		"app.kubernetes.io/instance":  "test",
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}, cr.SelectorLabels(vmv1beta1.ClusterComponentCommon))
	assert.Equal(t, map[string]string{
		"app.kubernetes.io/name":      "vmestimator-select",
		"app.kubernetes.io/part-of":   "vmestimator",
		"app.kubernetes.io/instance":  "test",
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
		"team":                        "observability",
	}, cr.FinalLabels(vmv1beta1.ClusterComponentSelect))
	assert.Equal(t, map[string]string{
		"app.kubernetes.io/name":      "vmestimator",
		"app.kubernetes.io/part-of":   "vmestimator",
		"app.kubernetes.io/instance":  "test",
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
		"team":                        "observability",
	}, cr.FinalLabels(vmv1beta1.ClusterComponentRoot))
}

func TestVMEstimator_UnmarshalJSON(t *testing.T) {
	f := func(data string, wantParsingErr bool) {
		t.Helper()
		var cr VMEstimator
		assert.NoError(t, json.Unmarshal([]byte(data), &cr))
		if wantParsingErr {
			assert.NotEmpty(t, cr.Status.ParsingSpecError)
		} else {
			assert.Empty(t, cr.Status.ParsingSpecError)
		}
	}

	// correct spec
	f(`{"metadata":{"name":"test"},"spec":{"streams":[{"interval":"5m","group_by":["job"],"calculate_churn":true}],"storage":{"replicaCount":2},"select":{}}}`, false)

	// unknown stream field
	f(`{"metadata":{"name":"test"},"spec":{"streams":[{"interval":"5m","groupBy":["job"]}]}}`, true)

	// unknown component field
	f(`{"metadata":{"name":"test"},"spec":{"storage":{"unknownField":true}}}`, true)
}

func TestVMEstimator_PodLabels(t *testing.T) {
	cr := &VMEstimator{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: VMEstimatorSpec{
			Storage: &VMEstimatorStorage{
				PodMetadata: &vmv1beta1.EmbeddedObjectMetadata{
					Labels: map[string]string{
						"team":                   "observability",
						"app.kubernetes.io/name": "custom",
					},
					Annotations: map[string]string{"owner": "sre"},
				},
			},
		},
	}
	// selector labels cannot be overridden by pod metadata
	assert.Equal(t, map[string]string{
		"app.kubernetes.io/name":      "vmestimator-storage",
		"app.kubernetes.io/instance":  "test",
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
		"team":                        "observability",
	}, cr.PodLabels(vmv1beta1.ClusterComponentStorage))
	assert.Equal(t, map[string]string{"owner": "sre"}, cr.PodAnnotations(vmv1beta1.ClusterComponentStorage))

	// components without pod metadata
	assert.Equal(t, cr.SelectorLabels(vmv1beta1.ClusterComponentSelect), cr.PodLabels(vmv1beta1.ClusterComponentSelect))
	assert.Nil(t, cr.PodAnnotations(vmv1beta1.ClusterComponentSelect))
}
