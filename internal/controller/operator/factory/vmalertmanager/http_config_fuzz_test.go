package vmalertmanager

import (
	"context"
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/VictoriaMetrics/operator/api/operator/fuzztest"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func FuzzBuildHTTPConfig(f *testing.F) {
	for _, seed := range []string{
		`{}`,
		`{"follow_redirects":true,"bearer_token_file":"/etc/vm/token"}`,
		`{"http_headers":{"X-Service":{"values":["api"]},"X-Environment":{"files":["/etc/vm/env"]}}}`,
		`{"bearer_token_secret":{"name":"creds","key":"token"}}`,
		`{"authorization":{"type":"Bearer","credentials":{"name":"creds","key":"token"}}}`,
		`{"http_headers":{"X-Secret":{"secrets":[{"name":"creds","key":"token"}]}}}`,
		`{"proxy_url":"http://proxy.example.org:8080","no_proxy":"localhost"}`,
	} {
		f.Add([]byte(seed))
	}
	f.Add([]byte("\"\\u\xa4\xa4\xa4\xa4"))
	f.Fuzz(func(t *testing.T, data []byte) {
		var httpCfg vmv1beta1.HTTPConfig
		if err := json.Unmarshal(data, &httpCfg); err != nil {
			fuzztest.New(data).Fuzz(&httpCfg)
		}
		scheme := runtime.NewScheme()
		if err := corev1.AddToScheme(scheme); err != nil {
			t.Fatal(err)
		}
		secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "creds", Namespace: "default"}, Data: map[string][]byte{"token": []byte("value")}}
		client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()
		am := fuzzAlertmanager()
		assertStableBuild(t, func() (any, error) {
			return buildHTTPConfig(&httpCfg, "default", getAssetsCache(context.Background(), client, am))
		})
	})
}
