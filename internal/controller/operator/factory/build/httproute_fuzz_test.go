package build

import (
	"encoding/json"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/VictoriaMetrics/operator/api/operator/fuzztest"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func FuzzHTTPRouteBuilder(f *testing.F) {
	for _, seed := range []string{
		`{}`,
		`\U00015b9e`,
		`{"port":"8427"}`,
		`{"port":"not-a-port"}`,
		`{"port":"8427","route":{"hostnames":["example.org"],"extraRules":[{"matches":[{"path":{"type":"Exact","value":"/api"}}]}]}}`,
		`{"port":"8427","route":{"extraRules":[{"filters":[{"type":"RequestHeaderModifier","requestHeaderModifier":{"add":[{"name":"X-Env","value":"test"}]}}]}]}}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var input struct {
			Port  string                      `json:"port"`
			Route vmv1beta1.EmbeddedHTTPRoute `json:"route"`
		}
		if err := json.Unmarshal(data, &input); err != nil {
			fuzztest.New(data).Fill(&input)
		}
		cr := &vmv1beta1.VMAuth{ObjectMeta: metav1.ObjectMeta{Name: "auth", Namespace: "default"}}
		_, _ = HTTPRoute(cr, input.Port, &input.Route)
	})
}
