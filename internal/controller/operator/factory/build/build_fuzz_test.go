package build

import (
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/VictoriaMetrics/operator/api/operator/fuzztest"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func FuzzMergeDeep(f *testing.F) {
	for _, seed := range []string{
		`{}`,
		`{"reverse":true}`,
		`{"cluster":{"base":{"retentionPeriod":"30d","vmselect":{"replicaCount":1,"extraArgs":{"keep":"x","override":"old"}}},"override":{"clusterVersion":"v1.2.3","vmselect":{"replicaCount":3,"extraArgs":{"override":"new","add":"y"}}}}}`,
		`{"reverse":true,"cluster":{"base":{"serviceAccountName":"base","vmselect":{"replicaCount":1}},"override":{"serviceAccountName":"zone","vminsert":{"replicaCount":2}}}}`,
		`{"cluster":{"base":{"vmselect":{"replicaCount":1}},"override":{"vmselect":null}}}`,
		`{"pod":{"base":{"containers":[{"name":"a","image":"x"}],"nodeSelector":{"a":"b"}},"override":{"containers":[{"name":"b"}],"nodeSelector":{"c":"d"},"hostNetwork":true}}}`,
		`{"reverse":true,"pod":{"base":{"securityContext":{"runAsUser":1}},"override":{"securityContext":{"runAsGroup":2}}}}`,
		`{"map":{"base":{"a":{"b":"1","c":"2"},"d":{"x":"y"}},"override":{"a":{"c":"new","z":"added"},"e":{"f":"1"}}}}`,
		`{"reverse":true,"map":{"base":{"a":{"b":"1"}},"override":{"a":{"b":"2"},"c":null}}}`,
		`{"reverse":true,"map":{"base":{"a":{"b":"1"}}}}`,
		`{"map":{"override":{"a":{"b":"1"}}}}`,
		`{"map":{"base":{"a":{"b":"1"}}}}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var input struct {
			Reverse bool `json:"reverse"`
			Cluster struct {
				Base     vmv1beta1.VMClusterSpec `json:"base"`
				Override vmv1beta1.VMClusterSpec `json:"override"`
			} `json:"cluster"`
			Pod struct {
				Base     corev1.PodSpec `json:"base"`
				Override corev1.PodSpec `json:"override"`
			} `json:"pod"`
			Map struct {
				Base     map[string]map[string]string `json:"base"`
				Override map[string]map[string]string `json:"override"`
			} `json:"map"`
		}
		if err := json.Unmarshal(data, &input); err != nil {
			fuzztest.New(data).Fill(&input)
		}
		MergeDeep(&input.Cluster.Base, &input.Cluster.Override, input.Reverse)
		MergeDeep(&input.Pod.Base, &input.Pod.Override, input.Reverse)
		MergeDeep(&input.Map.Base, &input.Map.Override, input.Reverse)
	})
}
