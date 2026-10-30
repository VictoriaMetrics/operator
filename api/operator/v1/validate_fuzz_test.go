package v1

import (
	"encoding/json"
	"testing"
)

func FuzzValidateCRs(f *testing.F) {
	for _, seed := range []string{
		`{}`,
		`{"spec":{"remoteWrite":[{"url":"http://localhost:9428/insert/native"}]}}`,
		`{"spec":{"insert":{"grpcSpec":{"listenPort":10428,"tlsConfig":{"cipherSuites":["TLS_AES_128_GCM_SHA256"]}}},"storage":{},"select":{}}}`,
		`{"spec":{"grpcSpec":{"listenPort":10428,"tlsConfig":{"cipherSuites":["TLS_AES_128_GCM_SHA256"]}}}}`,
		`{"spec":{"remoteWrite":[{"url":"http://localhost:9428/"}]}}`,
		`{"spec":{"vlinsert":{"syslogSpec":{"tcpListeners":[{"tlsConfig":{"minVersion":"TLS12"}}]}},"vlstorage":{},"vlselect":{}}}`,
		`{"spec":{"license":{"key":"test"}}}`,
		`{"cipherSuites":["TLS_AES_128_GCM_SHA256"]}`,
		`{"clientIDFile":"/client-id","clientSecretFile":"/client-secret","tokenURL":"https://example.com/token"}`,
		`{"caFile":"/etc/tls/ca.crt","caSecretKeyRef":{"name":"ca","key":"ca.crt"}}`,
		`{"tcpListeners":[{"tlsConfig":{"minVersion":"TLS12"}},{"tlsConfig":{"minVersion":"TLS13"}}]}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 4096 {
			t.Skip()
		}
		for _, v := range []interface{ Validate() error }{
			&VMAnomaly{}, &VTAgent{}, &VTCluster{}, &VTSingle{},
			&VLAgent{}, &VLCluster{}, &VLSingle{},
			&TLSServerConfig{}, &OAuth2{}, &TLSConfig{}, &SyslogServerSpec{},
		} {
			if err := json.Unmarshal(data, v); err != nil {
				continue
			}
			_ = v.Validate()
		}
	})
}
