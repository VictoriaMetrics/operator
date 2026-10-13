package v1

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

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
		Interval:      "15m",
		ChurnInterval: "15m",
		Filter:        `{job=~"api|worker",env!~"dev|staging"}`,
		GroupBy:       []string{"job", "__label__"},
		GroupLimit:    100,
		Buckets:       8,
		HLLPrecision:  18,
		HLLSparse:     ptr.To(false),
		Labels:        map[string]string{"cluster": "prod"},
	}, false)

	// incorrect interval
	f(VMEstimatorStream{Interval: "1d"}, true)
	f(VMEstimatorStream{Interval: "0s"}, true)

	// churn interval exceeds default interval
	f(VMEstimatorStream{ChurnInterval: "10m"}, true)

	// churn interval exceeds interval
	f(VMEstimatorStream{Interval: "5m", ChurnInterval: "6m"}, true)

	// incorrect churn interval
	f(VMEstimatorStream{ChurnInterval: "-1m"}, true)

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
		Mode:    VMEstimatorModeCluster,
		Storage: &VMEstimatorStorage{},
		Select:  &VMEstimatorSelect{},
	}, true)

	// single mode is used by default
	f(VMEstimatorSpec{
		Streams: streams,
	}, false)

	// streams are loaded from configmap only
	f(VMEstimatorSpec{
		StreamsConfigMap: &corev1.ConfigMapKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "streams"},
			Key:                  "streams.yaml",
		},
	}, false)

	// single mode
	f(VMEstimatorSpec{
		Mode:    VMEstimatorModeSingle,
		Streams: []VMEstimatorStream{{Interval: "5m", GroupBy: []string{"job"}}},
		Storage: &VMEstimatorStorage{},
	}, false)

	// cluster mode
	f(VMEstimatorSpec{
		Mode:    VMEstimatorModeCluster,
		Streams: streams,
		Storage: &VMEstimatorStorage{},
		Select:  &VMEstimatorSelect{},
	}, false)

	// select is ignored in single mode
	f(VMEstimatorSpec{
		Mode:    VMEstimatorModeSingle,
		Streams: streams,
		Select:  &VMEstimatorSelect{},
	}, false)

	// unsupported mode
	f(VMEstimatorSpec{
		Mode:    "replicated",
		Streams: streams,
	}, true)

	// incorrect stream
	f(VMEstimatorSpec{
		Streams: []VMEstimatorStream{{Interval: "5m"}, {Interval: "foo"}},
	}, true)

	// incorrect streams configmap
	f(VMEstimatorSpec{
		StreamsConfigMap: &corev1.ConfigMapKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "streams"},
		},
	}, true)

	// additional service name collides with the query, insert or storage service
	f(VMEstimatorSpec{
		Streams: streams,
		Storage: &VMEstimatorStorage{
			ServiceSpec: &vmv1beta1.AdditionalServiceSpec{
				EmbeddedObjectMetadata: vmv1beta1.EmbeddedObjectMetadata{Name: "vmestimator-test"},
			},
		},
	}, true)
	f(VMEstimatorSpec{
		Streams: streams,
		Storage: &VMEstimatorStorage{
			ServiceSpec: &vmv1beta1.AdditionalServiceSpec{
				EmbeddedObjectMetadata: vmv1beta1.EmbeddedObjectMetadata{Name: "vmestimator-insert-test"},
			},
		},
	}, true)
	f(VMEstimatorSpec{
		Mode:    VMEstimatorModeCluster,
		Streams: streams,
		Storage: &VMEstimatorStorage{
			ServiceSpec: &vmv1beta1.AdditionalServiceSpec{
				EmbeddedObjectMetadata: vmv1beta1.EmbeddedObjectMetadata{Name: "vmestimator-storage-test"},
			},
		},
	}, true)
	f(VMEstimatorSpec{
		Mode:    VMEstimatorModeCluster,
		Streams: streams,
		Select: &VMEstimatorSelect{
			ServiceSpec: &vmv1beta1.AdditionalServiceSpec{
				EmbeddedObjectMetadata: vmv1beta1.EmbeddedObjectMetadata{Name: "vmestimator-insert-test"},
			},
		},
	}, true)

	// single service isn't used by select nodes, so it could be overridden
	f(VMEstimatorSpec{
		Streams: streams,
		Storage: &VMEstimatorStorage{
			ServiceSpec: &vmv1beta1.AdditionalServiceSpec{
				UseAsDefault: true,
				Spec:         corev1.ServiceSpec{Type: corev1.ServiceTypeNodePort},
			},
		},
	}, false)

	// storage service must stay headless for select nodes
	f(VMEstimatorSpec{
		Mode:    VMEstimatorModeCluster,
		Streams: streams,
		Storage: &VMEstimatorStorage{
			ServiceSpec: &vmv1beta1.AdditionalServiceSpec{
				UseAsDefault: true,
				Spec:         corev1.ServiceSpec{Type: corev1.ServiceTypeNodePort},
			},
		},
	}, true)
	f(VMEstimatorSpec{
		Mode:    VMEstimatorModeCluster,
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

	// select requires storage nodes in cluster mode
	f(VMEstimatorSpec{
		Mode:    VMEstimatorModeCluster,
		Streams: streams,
		Storage: &VMEstimatorStorage{CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To[int32](0)}},
	}, true)
	f(VMEstimatorSpec{
		Mode:    VMEstimatorModeCluster,
		Streams: streams,
		Storage: &VMEstimatorStorage{CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To[int32](0)}},
		Select: &VMEstimatorSelect{CommonAppsParams: vmv1beta1.CommonAppsParams{
			ExtraArgs: map[string]string{"storageNode": "http://external-storage:8490"},
		}},
	}, false)

	// storage replicaCount is ignored in single mode
	f(VMEstimatorSpec{
		Streams: streams,
		Storage: &VMEstimatorStorage{CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To[int32](0)}},
	}, false)

	// incorrect select hpa
	f(VMEstimatorSpec{
		Mode:    VMEstimatorModeCluster,
		Streams: streams,
		Select: &VMEstimatorSelect{
			HPA: &vmv1beta1.EmbeddedHPA{MinReplicas: ptr.To(int32(5)), MaxReplicas: 2},
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
	assert.Equal(t, "vmestimator-insert-test", cr.PrefixedInsertName())
	assert.Equal(t, "vmestimator-test", cr.PrefixedQueryName())
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

func TestVMEstimator_URLs(t *testing.T) {
	f := func(spec VMEstimatorSpec, wantRemoteWriteURL string, wantURLs map[vmv1beta1.ClusterComponent]string) {
		t.Helper()
		cr := &VMEstimator{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "monitoring"},
			Spec:       spec,
		}
		assert.Equal(t, wantRemoteWriteURL, cr.RemoteWriteURL())
		for kind, wantURL := range wantURLs {
			assert.Equal(t, wantURL, cr.AsURL(kind), "kind=%s", kind)
		}
	}

	// remote write URL points to the insert service even if components aren't defaulted yet
	f(VMEstimatorSpec{}, "http://vmestimator-insert-test.monitoring.svc:8490/cardinality/api/v1/write", map[vmv1beta1.ClusterComponent]string{
		VMEstimatorComponentSingle:        "",
		vmv1beta1.ClusterComponentStorage: "",
		vmv1beta1.ClusterComponentSelect:  "",
	})

	// single mode with custom port, path prefix and tls
	f(VMEstimatorSpec{
		Storage: &VMEstimatorStorage{
			CommonAppsParams: vmv1beta1.CommonAppsParams{
				Port: "9000",
				ExtraArgs: map[string]string{
					"http.pathPrefix": "/estimator",
					"tls":             "true",
				},
			},
		},
	}, "https://vmestimator-insert-test.monitoring.svc:9000/estimator/cardinality/api/v1/write", map[vmv1beta1.ClusterComponent]string{
		VMEstimatorComponentSingle: "https://vmestimator-test.monitoring.svc:9000",
	})

	// remote write and query URLs are the same in cluster mode
	f(VMEstimatorSpec{
		Mode:    VMEstimatorModeCluster,
		Storage: &VMEstimatorStorage{},
		Select: &VMEstimatorSelect{
			CommonAppsParams: vmv1beta1.CommonAppsParams{Port: "8491"},
		},
	}, "http://vmestimator-insert-test.monitoring.svc:8490/cardinality/api/v1/write", map[vmv1beta1.ClusterComponent]string{
		vmv1beta1.ClusterComponentStorage: "http://vmestimator-storage-test.monitoring.svc:8490",
		vmv1beta1.ClusterComponentSelect:  "http://vmestimator-test.monitoring.svc:8491",
	})
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
	f(`{"metadata":{"name":"test"},"spec":{"mode":"cluster","streams":[{"interval":"5m","groupBy":["job"]}],"storage":{"replicaCount":2},"select":{}}}`, false)

	// vmestimator config format instead of CRD format
	f(`{"metadata":{"name":"test"},"spec":{"streams":[{"interval":"5m","group_by":["job"]}]}}`, true)

	// unknown component field
	f(`{"metadata":{"name":"test"},"spec":{"storage":{"unknownField":true}}}`, true)

	// single component is replaced with mode
	f(`{"metadata":{"name":"test"},"spec":{"single":{}}}`, true)
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

	// the single-replica Deployment uses spec.storage pod metadata
	assert.Equal(t, map[string]string{
		"app.kubernetes.io/name":      "vmestimator-single",
		"app.kubernetes.io/instance":  "test",
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
		"team":                        "observability",
	}, cr.PodLabels(VMEstimatorComponentSingle))
	assert.Equal(t, map[string]string{"owner": "sre"}, cr.PodAnnotations(VMEstimatorComponentSingle))

	// components without pod metadata
	assert.Equal(t, cr.SelectorLabels(vmv1beta1.ClusterComponentSelect), cr.PodLabels(vmv1beta1.ClusterComponentSelect))
	assert.Nil(t, cr.PodAnnotations(vmv1beta1.ClusterComponentSelect))
}

func TestVMEstimator_ValidateNames(t *testing.T) {
	f := func(name string, spec VMEstimatorSpec, wantErr bool) {
		t.Helper()
		cr := &VMEstimator{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec:       spec,
		}
		err := cr.Validate()
		if wantErr {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}
	}
	nameOfLen := func(n int) string {
		return strings.Repeat("a", n)
	}
	streams := []VMEstimatorStream{{Interval: "5m"}}
	single := VMEstimatorSpec{Streams: streams}
	cluster := VMEstimatorSpec{Mode: VMEstimatorModeCluster, Streams: streams}

	// insert Service name vmestimator-insert-<name> must not exceed 63 chars
	f(nameOfLen(44), single, false)
	f(nameOfLen(45), single, true)

	// storage StatefulSet name vmestimator-storage-<name> must not exceed 52 chars
	f(nameOfLen(32), cluster, false)
	f(nameOfLen(33), cluster, true)

	// dots are allowed at object names, but not at Service names
	f("my.estimator", single, true)
}

func TestVMEstimator_RemoteWriteURLWithServiceOverride(t *testing.T) {
	cr := &VMEstimator{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "monitoring"},
		Spec: VMEstimatorSpec{
			Storage: &VMEstimatorStorage{
				ServiceSpec: &vmv1beta1.AdditionalServiceSpec{
					UseAsDefault: true,
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{{Name: "http", Port: 80}},
					},
				},
			},
		},
	}
	// insert service inherits ports of the default service in both modes
	assert.Equal(t, "http://vmestimator-insert-test.monitoring.svc:80/cardinality/api/v1/write", cr.RemoteWriteURL())
	cr.Spec.Mode = VMEstimatorModeCluster
	assert.Equal(t, "http://vmestimator-insert-test.monitoring.svc:80/cardinality/api/v1/write", cr.RemoteWriteURL())
}
