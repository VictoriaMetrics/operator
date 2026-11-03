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

type validatorKind byte

const (
	vmAnomalyKind validatorKind = iota
	vtAgentKind
	vtClusterKind
	vtSingleKind
	vlAgentKind
	vlClusterKind
	vlSingleKind
	tlsServerConfigKind
	oauth2Kind
	tlsConfigKind
	syslogServerSpecKind
)

func FuzzValidateCRs(f *testing.F) {
	for _, seed := range []struct {
		kind validatorKind
		data string
	}{
		{vmAnomalyKind, `{}`},
		{vmAnomalyKind, `{"metadata":{"annotations":{"operator.victoriametrics.com/skip-validation":"true"}}}`},
		{vmAnomalyKind, `{"spec":{"license":{"key":"k"},"vpa":{}}}`},
		{vmAnomalyKind, `{"spec":{"license":{"key":"k"},"preStopSleepSeconds":30,"terminationGracePeriodSeconds":10}}`},
		{vmAnomalyKind, `{"spec":{"license":{"key":"k"}}}`},
		{vtAgentKind, `{"spec":{"remoteWrite":[{"url":"http://localhost:9428/insert/native"}]}}`},
		{vtAgentKind, `{"metadata":{"name":"a"},"spec":{"serviceSpec":{"metadata":{"name":"vtagent-a"}}}}`},
		{vtAgentKind, `{"spec":{"remoteWrite":[{}]}}`},
		{vtAgentKind, `{"spec":{"remoteWrite":[{"url":"http://a","tlsConfig":{"caFile":"/ca","caSecretKeyRef":{"name":"a","key":"b"}}}]}}`},
		{vtAgentKind, `{"spec":{"remoteWrite":[{"url":"http://a"}],"preStopSleepSeconds":30,"terminationGracePeriodSeconds":10}}`},
		{vtAgentKind, `{"metadata":{"annotations":{"operator.victoriametrics.com/skip-validation":"true"}}}`},
		{vtAgentKind, `{"spec":{"remoteWrite":[{"url":"http://a"}],"vpa":{}}}`},
		{vtAgentKind, `{"spec":{"remoteWrite":[{"url":"http://a"}],"grpcSpec":{"tlsConfig":{"cipherSuites":["bad"]}}}}`},
		{vtClusterKind, `{"spec":{"insert":{"grpcSpec":{"listenPort":10428,"tlsConfig":{"cipherSuites":["TLS_AES_128_GCM_SHA256"]}}},"storage":{},"select":{}}}`},
		{vtClusterKind, `{"metadata":{"name":"a"},"spec":{"insert":{"serviceSpec":{"metadata":{"name":"vtinsert-a"}}}}}`},
		{vtClusterKind, `{"metadata":{"name":"a"},"spec":{"storage":{"serviceSpec":{"metadata":{"name":"vtstorage-a"}}}}}`},
		{vtClusterKind, `{"metadata":{"name":"a"},"spec":{"select":{"serviceSpec":{"metadata":{"name":"vtselect-a"}}}}}`},
		{vtClusterKind, `{"metadata":{"name":"a"},"spec":{"requestsLoadBalancer":{"enabled":true,"spec":{"serviceSpec":{"metadata":{"name":"vtclusterlb-a"}}}}}}`},
		{vtClusterKind, `{"spec":{"insert":{"grpcSpec":{"tlsConfig":{"cipherSuites":["bad"]}}}}}`},
		{vtClusterKind, `{"spec":{"storage":{"hpa":{"behaviour":{"scaleDown":{}}}}}}`},
		{vtClusterKind, `{"spec":{"select":{"extraArgs":{"storageNode":"a, b,a"}}}}`},
		{vtClusterKind, `{"spec":{"select":{"extraArgs":{"storageNode":"a"},"extraStorageNodes":[{"addr":"b"},{"addr":"a"}]}}}`},
		{vtClusterKind, `{"spec":{"insert":{"port":"9999","preStopSleepSeconds":30,"terminationGracePeriodSeconds":10}}}`},
		{vtClusterKind, `{"spec":{"storage":{"vpa":{}}}}`},
		{vtClusterKind, `{"spec":{"storage":{"preStopSleepSeconds":30,"terminationGracePeriodSeconds":10}}}`},
		{vtClusterKind, `{"spec":{"select":{"hpa":{"minReplicas":3,"maxReplicas":1}}}}`},
		{vtClusterKind, `{"spec":{"select":{"vpa":{}}}}`},
		{vtClusterKind, `{"spec":{"select":{"preStopSleepSeconds":30,"terminationGracePeriodSeconds":10}}}`},
		{vtClusterKind, `{"metadata":{"annotations":{"operator.victoriametrics.com/skip-validation":"true"}}}`},
		{vtSingleKind, `{"spec":{"grpcSpec":{"listenPort":10428,"tlsConfig":{"cipherSuites":["TLS_AES_128_GCM_SHA256"]}}}}`},
		{vtSingleKind, `{"metadata":{"name":"a"},"spec":{"serviceSpec":{"metadata":{"name":"vtsingle-a"}}}}`},
		{vtSingleKind, `{"metadata":{"annotations":{"operator.victoriametrics.com/skip-validation":"true"}}}`},
		{vtSingleKind, `{}`},
		{vtSingleKind, `{"spec":{"preStopSleepSeconds":30,"terminationGracePeriodSeconds":10}}`},
		{vlAgentKind, `{"spec":{"remoteWrite":[{"url":"http://localhost:9428/"}]}}`},
		{vlAgentKind, `{"spec":{"remoteWrite":[{"url":"http://localhost:9428/"}],"preStopSleepSeconds":30,"terminationGracePeriodSeconds":10}}`},
		{vlAgentKind, `{"spec":{"k8sCollector":{"enabled":true,"extraFields":"{"},"remoteWrite":[{"url":"http://a"}]}}`},
		{vlAgentKind, `{"spec":{"k8sCollector":{"enabled":true},"vpa":{},"remoteWrite":[{"url":"http://a"},{}]}}`},
		{vlAgentKind, `{"spec":{"remoteWrite":[{"url":"http://a","oauth2":{}}]}}`},
		{vlAgentKind, `{"spec":{}}`},
		{vlAgentKind, `{"spec":{"remoteWrite":[{}],"vpa":{}}}`},
		{vlAgentKind, `{"spec":{"remoteWrite":[{"url":"http://a"}],"vpa":{}}}`},
		{vlAgentKind, `{"metadata":{"annotations":{"operator.victoriametrics.com/skip-validation":"true"}}}`},
		{vlAgentKind, `{"metadata":{"name":"a"},"spec":{"serviceSpec":{"metadata":{"name":"vlagent-a"}}}}`},
		{vlAgentKind, `{"spec":{"remoteWrite":[{"url":"http://a"}],"syslogSpec":{"tcpListeners":[{"tlsConfig":{"cipherSuites":["bad"]}}]}}}`},
		{vlClusterKind, `{"spec":{"vlinsert":{"syslogSpec":{"tcpListeners":[{"tlsConfig":{"minVersion":"TLS12"}}]}},"vlstorage":{},"vlselect":{}}}`},
		{vlClusterKind, `{"metadata":{"name":"a"},"spec":{"vlinsert":{"serviceSpec":{"metadata":{"name":"vlinsert-a"}}}}}`},
		{vlClusterKind, `{"metadata":{"name":"a"},"spec":{"vlstorage":{"serviceSpec":{"metadata":{"name":"vlstorage-a"}}}}}`},
		{vlClusterKind, `{"metadata":{"name":"a"},"spec":{"vlselect":{"serviceSpec":{"metadata":{"name":"vlselect-a"}}}}}`},
		{vlClusterKind, `{"metadata":{"name":"a"},"spec":{"requestsLoadBalancer":{"enabled":true,"spec":{"serviceSpec":{"metadata":{"name":"vlclusterlb-a"}}}}}}`},
		{vlClusterKind, `{"spec":{"vlinsert":{"syslogSpec":{"tcpListeners":[{"tlsConfig":{"cipherSuites":["bad"]}}]}}}}`},
		{vlClusterKind, `{"spec":{"vlstorage":{"hpa":{"behaviour":{"scaleDown":{}}}}}}`},
		{vlClusterKind, `{"spec":{"vlselect":{"extraArgs":{"storageNode":"a, b,a"}}}}`},
		{vlClusterKind, `{"spec":{"vlselect":{"extraArgs":{"storageNode":"a"},"extraStorageNodes":[{"addr":"b"},{"addr":"a"}]}}}`},
		{vlClusterKind, `{"spec":{"vlinsert":{"preStopSleepSeconds":30,"terminationGracePeriodSeconds":10}}}`},
		{vlClusterKind, `{"spec":{"vlstorage":{"vpa":{}}}}`},
		{vlClusterKind, `{"spec":{"vlstorage":{"preStopSleepSeconds":30,"terminationGracePeriodSeconds":10}}}`},
		{vlClusterKind, `{"spec":{"vlselect":{"hpa":{"minReplicas":3,"maxReplicas":1}}}}`},
		{vlClusterKind, `{"spec":{"vlselect":{"vpa":{}}}}`},
		{vlClusterKind, `{"spec":{"vlselect":{"preStopSleepSeconds":30,"terminationGracePeriodSeconds":10}}}`},
		{vlClusterKind, `{"metadata":{"annotations":{"operator.victoriametrics.com/skip-validation":"true"}}}`},
		{vlSingleKind, `{"spec":{"license":{"key":"test"}}}`},
		{vlSingleKind, `{"metadata":{"name":"a"},"spec":{"serviceSpec":{"metadata":{"name":"vlsingle-a"}}}}`},
		{vlSingleKind, `{"spec":{"preStopSleepSeconds":30,"terminationGracePeriodSeconds":10}}`},
		{vlSingleKind, `{"spec":{"syslogSpec":{"tcpListeners":[{"tlsConfig":{"cipherSuites":["bad"]}}]}}}`},
		{vlSingleKind, `{"metadata":{"annotations":{"operator.victoriametrics.com/skip-validation":"true"}}}`},
		{tlsServerConfigKind, `{"cipherSuites":["TLS_AES_128_GCM_SHA256"]}`},
		{tlsServerConfigKind, `{"cipherSuites":["0x1301","0xffff"]}`},
		{oauth2Kind, `{"clientIDFile":"/client-id","clientSecretFile":"/client-secret","tokenURL":"https://example.com/token"}`},
		{oauth2Kind, `{"clientSecretFile":"/s"}`},
		{oauth2Kind, `{"clientIDFile":"/i","clientSecretFile":"/s","clientSecret":{"name":"a","key":"b"},"tokenURL":":"}`},
		{oauth2Kind, `{"clientIDFile":"/i","clientSecretFile":"/s","tokenURL":":"}`},
		{oauth2Kind, `{"clientIDFile":"/client-id","tokenURL":"https://example.com/token"}`},
		{oauth2Kind, `{"clientIDFile":"/client-id","clientSecretFile":"/client-secret","tokenURL":"https://example.com/token","endpointParams":{"a":"b"}}`},
		{tlsConfigKind, `{"caFile":"/etc/tls/ca.crt","caSecretKeyRef":{"name":"ca","key":"ca.crt"}}`},
		{tlsConfigKind, `{"certFile":"/c","certSecretKeyRef":{"name":"a","key":"b"}}`},
		{tlsConfigKind, `{"keyFile":"/k","keySecretKeyRef":{"name":"a","key":"b"}}`},
		{tlsConfigKind, `{"caFile":"/ca"}`},
		{syslogServerSpecKind, `{"tcpListeners":[{"tlsConfig":{"minVersion":"TLS12"}},{"tlsConfig":{"minVersion":"TLS13"}}]}`},
		{syslogServerSpecKind, `{"tcpListeners":[null,{"tlsConfig":{"cipherSuites":["TLS_AES_128_GCM_SHA256"]}},{"tlsConfig":{"cipherSuites":["TLS_AES_256_GCM_SHA384"]}}]}`},
		{syslogServerSpecKind, `{"tcpListeners":[{"streamFields":"[\"a\"]","ignoreFields":"a","decolorizeFields":1}]}`},
	} {
		f.Add(append([]byte{byte(seed.kind)}, seed.data...))
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
		fuzztest.New(data[1:]).MaxDepth(8).Fill(v)
		_ = v.Validate()
	})
}
