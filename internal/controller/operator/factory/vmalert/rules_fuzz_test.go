package vmalert

import (
	"bytes"
	"encoding/json"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/VictoriaMetrics/operator/api/operator/fuzztest"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func FuzzRuleConfig(f *testing.F) {
	for _, seed := range []string{
		`{}`,
		`{"groups":[{"name":"recording","rules":[{"record":"up:sum","expr":"sum(up)"}]}]}`,
		`{"groups":[{"name":"alerts","rules":[{"alert":"Down","expr":"up == 0","labels":{"severity":"critical"}}]}]}`,
		`{"groups":[{"name":"g","rules":[{"record":"a","expr":"up"}]},{"name":"g","rules":[{"alert":"b","expr":"up"}]},{"name":"h","rules":[]}]}`,
		`{"groups":[{"name":"full","interval":"1m","limit":5,"eval_offset":"1s","eval_delay":"2s","eval_alignment":true,"concurrency":2,"labels":{"a":"b"},"tenant":"1","params":{"x":["y"]},"rules":[{"alert":"A","expr":"up == 0","for":"5m","keep_firing_for":"1m","debug":true,"labels":{"l":"v"},"annotations":{"summary":"s"},"update_entries_limit":10}]}]}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var spec vmv1beta1.VMRuleSpec
		if err := json.Unmarshal(data, &spec); err != nil {
			fuzztest.New(data).Fill(&spec)
		}
		render := func() ([][]byte, error) {
			cr := &vmv1beta1.VMAlert{ObjectMeta: metav1.ObjectMeta{Name: "alert", Namespace: "default"}}
			cms, err := makeRulesConfigMaps(cr, spec.DeepCopy().Groups)
			if err != nil {
				return nil, err
			}
			var result [][]byte
			for _, cm := range cms {
				result = append(result, cm.BinaryData[rulesFilename])
			}
			return result, nil
		}
		a, err := render()
		b, err2 := render()
		if (err == nil) != (err2 == nil) || err != nil && err.Error() != err2.Error() || len(a) != len(b) {
			t.Fatalf("non-idempotent rule config: %v != %v", err, err2)
		}
		for i := range a {
			if !bytes.Equal(a[i], b[i]) {
				t.Fatalf("non-idempotent rule config bucket %d", i)
			}
		}
	})
}
