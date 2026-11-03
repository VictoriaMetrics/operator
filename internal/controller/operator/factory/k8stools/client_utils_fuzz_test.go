package k8stools

import (
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/VictoriaMetrics/operator/api/operator/fuzztest"
)

type mergePatchInput struct {
	Base    []corev1.Container
	Patches []corev1.Container
}

func FuzzMergePatchContainers(f *testing.F) {
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"Base":[{"name":"a","image":"i"}],"Patches":[{"name":"a","image":"j"}]}`))
	f.Add([]byte(`{"Base":[{"name":"a"}],"Patches":[{"name":"b"}]}`))
	f.Add([]byte(`{"Base":[{"name":"a"},{"name":"a"}],"Patches":[{"name":"a"},{"name":"a"}]}`))
	f.Add([]byte(`{"Base":[{"name":"a","env":[{"name":"X","value":"1"}],"ports":[{"containerPort":1}]}],"Patches":[{"name":"a","env":[{"name":"X","value":"2"},{"name":"Y"}],"ports":[{"containerPort":1,"protocol":"UDP"}]}]}`))
	f.Add([]byte(`{"Base":[{"name":"a","volumeMounts":[{"name":"v","mountPath":"/x"}],"args":["-a"]}],"Patches":[{"name":"a","volumeMounts":[{"name":"w","mountPath":"/x"}],"args":["-b"],"resources":{"limits":{"cpu":"1"}}}]}`))
	f.Add([]byte(`{"Base":[{"name":"a"}],"Patches":[{"name":"a","$patch":"delete"}]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		var in mergePatchInput
		if err := json.Unmarshal(data, &in); err != nil {
			fuzztest.New(data).Fill(&in)
		}
		// error is expected for unmergeable containers, only panics matter
		_, _ = MergePatchContainers(in.Base, in.Patches)
	})
}
