package converter

import (
	"context"
	"testing"

	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
)

func FuzzPrometheusRelabelConversion(f *testing.F) {
	for _, seed := range []string{
		`null`,
		`[]`,
		`[{"action":"keep","sourceLabels":["__name__"],"regex":"up|down"}]`,
		`[{"action":"drop"},{"action":"replace","targetLabel":"job","replacement":"api"}]`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		fuzzPromConversion(t, data, func(src *[]promv1.RelabelConfig) any {
			return ConvertRelabelConfig(context.Background(), *src)
		})
	})
}

func FuzzPrometheusAuthConversion(f *testing.F) {
	for _, seed := range []string{
		`{}`,
		`{"tlsConfig":{"caFile":"/etc/prometheus/secrets/ca","certFile":"/etc/prometheus/configmaps/cert"}}`,
		`{"safeTLSConfig":{"serverName":"example.org","insecureSkipVerify":true}}`,
		`{"oauth":{"clientID":{"secret":{"name":"oauth","key":"id"}},"clientSecret":{"name":"oauth","key":"secret"},"tokenUrl":"https://example.org/token"}}`,
		`{"basicAuth":{"username":{"name":"auth","key":"user"},"password":{"name":"auth","key":"pass"}}}`,
		`{"authorization":{"type":"Bearer","credentialsFile":"/etc/prometheus/secrets/token"},"proxyConfig":{"proxyUrl":"http://proxy:8080"}}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		type authInput struct {
			TLSConfig         *promv1.TLSConfig         `json:"tlsConfig"`
			SafeTLSConfig     *promv1.SafeTLSConfig     `json:"safeTLSConfig"`
			OAuth             *promv1.OAuth2            `json:"oauth"`
			BasicAuth         *promv1.BasicAuth         `json:"basicAuth"`
			Authorization     *promv1.Authorization     `json:"authorization"`
			SafeAuthorization *promv1.SafeAuthorization `json:"safeAuthorization"`
			ProxyConfig       promv1.ProxyConfig        `json:"proxyConfig"`
		}
		fuzzPromConversion(t, data, func(src *authInput) any {
			return []any{
				ConvertTLSConfig(src.TLSConfig), ConvertSafeTLSConfig(src.SafeTLSConfig),
				ConvertOAuth(src.OAuth), ConvertBasicAuth(src.BasicAuth),
				ConvertAuthorization(src.SafeAuthorization, src.Authorization), ConvertProxyConfig(&src.ProxyConfig),
			}
		})
	})
}
