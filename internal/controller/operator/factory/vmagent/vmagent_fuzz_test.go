package vmagent

import (
	"encoding/json"
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/VictoriaMetrics/operator/api/operator/fuzztest"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
)

func FuzzRemoteWriteArgs(f *testing.F) {
	for _, seed := range []string{
		`{}`,
		`{"remoteWrite":[{"url":"http://one:8428/api/v1/write"}]}`,
		`{"remoteWrite":[{"url":"http://one/api/v1/write","sendTimeout":"30s"},{"url":"https://two/api/v1/write"}],"remoteWriteSettings":{"queues":2}}`,
		`{"remoteWrite":[{"url":"http://one","basicAuth":{"username":{"name":"missing","key":"username"}}}]}`,
		`{"statefulMode":true,"remoteWrite":[{"url":"http://one","inlineUrlRelabelConfig":[{"sourceLabels":["job"],"targetLabel":"job"}]},{"url":"http://two"}]}`,
		`{"remoteWrite":[{"url":"http://one","tlsConfig":{"caFile":"/ca","certFile":"/c","keyFile":"/k","serverName":"s","insecureSkipVerify":true},"oauth2":{"client_id":{},"token_url":"http://t","scopes":["a","b"],"client_secret_file":"/s"},"aws":{"region":"r","useSigv4":true},"headers":["a:b","c:d"],"proxyURL":"http://p","forceVMProto":true,"queues":3,"maxDiskUsage":"1GB","sendTimeout":"5s","streamAggrConfig":{"rules":[{"outputs":["total"],"interval":"1m"}]}},{"url":"http://two"}],"remoteWriteSettings":{"flushInterval":"1s","showURL":true,"tmpDataPath":"/tmp/q","maxBlockSize":1024,"label":{"b":"2","a":"1"},"useMultiTenantMode":true,"maxDiskUsagePerURL":"2GB"}}`,
		`{"statefulMode":true,"statefulStorage":{"volumeClaimTemplate":{"spec":{"resources":{"requests":{"storage":"10Gi"}}}}},"remoteWrite":[{"url":"http://one","basicAuth":{"password_file":"/p","username":{"name":"s","key":"u"}}},{"url":"http://two"}],"remoteWriteSettings":{"queues":4}}`,
	} {
		f.Add([]byte(seed))
	}
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		f.Fatal(err)
	}
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	f.Fuzz(func(t *testing.T, data []byte) {
		var spec vmv1beta1.VMAgentSpec
		if err := json.Unmarshal(data, &spec); err != nil {
			fuzztest.New(data).Fuzz(&spec)
		}
		render := func() ([]string, error) {
			return buildRemoteWriteArgs(&vmv1beta1.VMAgent{Spec: *spec.DeepCopy()}, build.NewAssetsCache(t.Context(), client, nil))
		}
		a, err := render()
		b, err2 := render()
		if (err == nil) != (err2 == nil) || !reflect.DeepEqual(a, b) {
			t.Fatalf("non-idempotent remoteWrite: %q (%v) != %q (%v)", a, err, b, err2)
		}
	})
}
