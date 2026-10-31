package v1

import (
	"encoding/json"
	"testing"

	"github.com/VictoriaMetrics/operator/api/operator/fuzztest"
)

func fuzzValidator(selector byte) interface{ Validate() error } {
	validators := [...]interface{ Validate() error }{
		&VMAnomaly{}, &VTAgent{}, &VTCluster{}, &VTSingle{},
		&VLAgent{}, &VLCluster{}, &VLSingle{},
		&TLSServerConfig{}, &OAuth2{}, &TLSConfig{}, &SyslogServerSpec{},
	}
	return validators[int(selector)%len(validators)]
}

func FuzzValidateCRs(f *testing.F) {
	for i, seed := range []string{
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
		f.Add(append([]byte{byte(i)}, seed...))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 {
			return
		}
		parsed := fuzzValidator(data[0])
		if err := json.Unmarshal(data[1:], parsed); err == nil {
			_ = parsed.Validate()
		}
		v := fuzzValidator(data[0])
		// Limit depth past unsupported interfaces.
		fuzztest.New(data[1:]).Fuzz(v)
		_ = v.Validate()
	})
}
