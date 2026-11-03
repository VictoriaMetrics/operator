package build

import (
	"encoding/json"
	"slices"
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/VictoriaMetrics/operator/api/operator/fuzztest"
	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func FuzzInsertPorts(f *testing.F) {
	for _, seed := range []string{
		`{}`,
		`{"ip":{"graphitePort":"2003","influxPort":"8089","openTSDBPort":"4242","openTSDBHTTPPort":"4243"},"svc":{}}`,
		`{"ip":{"graphitePort":"graphite","influxPort":"-1","openTSDBPort":"99999999999","openTSDBHTTPPort":" "},"svc":{}}`,
		`{"ip":{"graphitePort":"2003","influxPort":"8089","openTSDBPort":"4242","openTSDBHTTPPort":"4243"},"svc":{"spec":{"ports":[{"name":"graphite-tcp","port":1},{"name":"influx-udp","port":2},{"name":"opentsdb-http","port":3}]}}}`,
		`{"ip":{"influxPort":"8089"},"ports":[{"name":"http","containerPort":8480}],"args":["-envflag.enable"]}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var input struct {
			IP    *vmv1beta1.InsertPorts `json:"ip"`
			Ports []corev1.ContainerPort `json:"ports"`
			Args  []string               `json:"args"`
			Svc   *corev1.Service        `json:"svc"`
		}
		if err := json.Unmarshal(data, &input); err != nil {
			fuzztest.New(data).Fill(&input)
		}
		AppendInsertPorts(input.Ports, input.IP)
		AppendArgsForInsertPorts(input.Args, input.IP)
		AppendInsertPortsToService(input.IP, input.Svc)
	})
}

func FuzzSyslogBuilders(f *testing.F) {
	for _, seed := range []string{
		`{}`,
		`{"spec":{}}`,
		`{"spec":{"tcpListeners":[{"listenPort":514}],"udpListeners":[{"listenPort":515}]}}`,
		`{"spec":{"tcpListeners":[{"listenPort":514,"streamFields":"[\"a\",\"b\"]","ignoreFields":"[\"c\"]","decolorizeFields":"[\"d\"]","tenantID":"1:2","compressMethod":"zstd"}],"udpListeners":[{"listenPort":515,"streamFields":"[\"a\"]","ignoreFields":"[\"b\"]","decolorizeFields":"[\"c\"]","tenantID":"3:4","compressMethod":"gzip"}]}}`,
		`{"spec":{"tcpListeners":[{"listenPort":514,"tlsConfig":{"certFile":"/c.pem","keyFile":"/k.pem","minVersion":"TLS12","cipherSuites":["A","B"]}},{"listenPort":515}]},"mountPath":"/tls"}`,
		`{"spec":{"tcpListeners":[{"listenPort":514,"tlsConfig":{"certSecret":{"name":"s","key":"c"},"keySecret":{"name":"s","key":"k"},"cipherSuites":["A"]}},{"listenPort":515,"tlsConfig":{"certSecret":{"name":"t","key":"c"},"keySecret":{"name":"t","key":"k"},"cipherSuites":["A","B"]}}]},"mountPath":"/tls"}`,
		`{"spec":{"tcpListeners":[{"listenPort":514,"tlsConfig":{"minVersion":"TLS12","cipherSuites":["A","B"]}},{"listenPort":515,"tlsConfig":{"minVersion":"TLS12","cipherSuites":["A","B"]}}]},"mountPath":"/tls"}`,
		`{"spec":{"tcpListeners":[{"listenPort":514,"tlsConfig":{}}]}}`,
		`{"spec":{"tcpListeners":[{"listenPort":514,"tlsConfig":{"certSecret":{"name":"s","key":"c"}}}]},"ports":[{"name":"http","containerPort":80}],"args":["-a"],"volumes":[{"name":"secret-tls-s"}],"mounts":[{"name":"secret-tls-s"}],"svc":{"spec":{"ports":[{"name":"http","port":80}]}}}`,
		`{"spec":{"tcpListeners":[{"listenPort":-1}],"udpListeners":[{"listenPort":2147483647}]}}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var input struct {
			Spec      *vmv1.SyslogServerSpec `json:"spec"`
			MountPath string                 `json:"mountPath"`
			Ports     []corev1.ContainerPort `json:"ports"`
			Args      []string               `json:"args"`
			Volumes   []corev1.Volume        `json:"volumes"`
			Mounts    []corev1.VolumeMount   `json:"mounts"`
			Svc       corev1.Service         `json:"svc"`
		}
		if err := json.Unmarshal(data, &input); err != nil {
			fuzztest.New(data).Fill(&input)
		}
		dropNilListeners(input.Spec)
		AddSyslogPortsTo(input.Ports, input.Spec)
		AddSyslogArgsTo(input.Args, input.Spec, input.MountPath)
		AddSyslogTLSConfigToVolumes(input.Volumes, input.Mounts, input.Spec, input.MountPath)
		AddSyslogPortsToService(&input.Svc, input.Spec)
	})
}

// dropNilListeners removes null list items, the API server rejects them.
func dropNilListeners(spec *vmv1.SyslogServerSpec) {
	if spec == nil {
		return
	}
	spec.TCPListeners = slices.DeleteFunc(spec.TCPListeners, func(l *vmv1.SyslogTCPListener) bool { return l == nil })
	spec.UDPListeners = slices.DeleteFunc(spec.UDPListeners, func(l *vmv1.SyslogUDPListener) bool { return l == nil })
}

func FuzzOTLPGRPCBuilders(f *testing.F) {
	for _, seed := range []string{
		`{}`,
		`{"spec":{"listenPort":4317}}`,
		`{"spec":{"listenPort":4317,"tlsConfig":{"certFile":"/c.pem","keyFile":"/k.pem","minVersion":"TLS12","cipherSuites":["A","B"]}},"mountPath":"/tls"}`,
		`{"spec":{"listenPort":4317,"tlsConfig":{"certSecret":{"name":"s","key":"c"},"keySecret":{"name":"t","key":"k"}}},"mountPath":"/tls"}`,
		`{"spec":{"listenPort":4317,"tlsConfig":{"certSecret":{"name":"s","key":"c"},"keySecret":{"name":"s","key":"k"}}}}`,
		`{"spec":{"listenPort":4317,"tlsConfig":{"certFile":"/c.pem","certSecret":{"name":"s","key":"c"}}}}`,
		`{"spec":{"listenPort":4317,"tlsConfig":{}}}`,
		`{"spec":{"listenPort":4317,"tlsConfig":{"certSecret":{"name":"s","key":"c"}}},"ports":[{"name":"http","containerPort":80}],"args":["-a"],"volumes":[{"name":"secret-tls-s"}],"mounts":[{"name":"secret-tls-s"}],"svc":{"spec":{"ports":[{"name":"http","port":80}]}}}`,
		`{"spec":{"listenPort":-1}}`,
		`{"spec":{"listenPort":2147483647}}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var input struct {
			Spec      *vmv1.OTLPGRPCSpec     `json:"spec"`
			MountPath string                 `json:"mountPath"`
			Ports     []corev1.ContainerPort `json:"ports"`
			Args      []string               `json:"args"`
			Volumes   []corev1.Volume        `json:"volumes"`
			Mounts    []corev1.VolumeMount   `json:"mounts"`
			Svc       corev1.Service         `json:"svc"`
		}
		if err := json.Unmarshal(data, &input); err != nil {
			fuzztest.New(data).Fill(&input)
		}
		AddOTLPGRPCPortTo(input.Ports, input.Spec)
		AddOTLPGRPCArgsTo(input.Args, input.Spec, input.MountPath)
		AddOTLPGRPCTLSConfigToVolumes(input.Volumes, input.Mounts, input.Spec, input.MountPath)
		AddOTLPGRPCPortToService(&input.Svc, input.Spec)
	})
}
