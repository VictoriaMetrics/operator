package vmagent

import (
	"encoding/json"
	"reflect"
	"testing"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
)

func FuzzRemoteWriteArgs(f *testing.F) {
	for _, seed := range []string{
		`{}`,
		`{"remoteWrite":[{"url":"http://one:8428/api/v1/write"}]}`,
		`{"remoteWrite":[{"url":"http://one/api/v1/write","sendTimeout":"30s"},{"url":"https://two/api/v1/write"}],"remoteWriteSettings":{"queues":2}}`,
		`{"statefulMode":true,"remoteWrite":[{"url":"http://one","inlineUrlRelabelConfig":[{"sourceLabels":["job"],"targetLabel":"job"}]},{"url":"http://two"}]}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 4096 {
			t.Skip()
		}
		var spec vmv1beta1.VMAgentSpec
		if json.Unmarshal(data, &spec) != nil {
			return
		}
		render := func() ([]string, error) {
			return buildRemoteWriteArgs(&vmv1beta1.VMAgent{Spec: *spec.DeepCopy()}, build.NewAssetsCache(t.Context(), nil, nil))
		}
		a, err := render()
		b, err2 := render()
		if (err == nil) != (err2 == nil) || !reflect.DeepEqual(a, b) {
			t.Fatalf("non-idempotent remoteWrite: %q (%v) != %q (%v)", a, err, b, err2)
		}
	})
}
