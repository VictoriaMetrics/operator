package build

import (
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/VictoriaMetrics/operator/api/operator/fuzztest"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

type probeStub struct {
	Path     string `json:"path"`
	Scheme   string `json:"scheme"`
	Port     string `json:"port"`
	Liveness bool   `json:"liveness"`
	Proxy    bool   `json:"proxy"`
}

func (p probeStub) ProbePath() string       { return p.Path }
func (p probeStub) ProbeScheme() string     { return p.Scheme }
func (p probeStub) ProbePort() string       { return p.Port }
func (p probeStub) ProbeNeedLiveness() bool { return p.Liveness }
func (p probeStub) UseProxyProtocol() bool  { return p.Proxy }

func FuzzConfigReloaderContainer(f *testing.F) {
	for _, seed := range []string{
		`{}`,
		`{"agent":{"spec":{"port":"8429"}},"mounts":[{"name":"config-out","mountPath":"/etc/vm/config"},{"name":"extra","mountPath":"/etc/vm/extra"}]}`,
		`{"agent":{"spec":{"port":"8429","configReloaderExtraArgs":{"web.listen-address":":8429"}}},"init":true}`,
		`{"secret":{"name":"config","key":"agent.yml"},"mounts":[{"name":"config-out","mountPath":"/etc/vm/config"}]}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		// Use custom struct as `reloader` is an interface.
		var input struct {
			Agent  vmv1beta1.VMAgent         `json:"agent"`
			Mounts []corev1.VolumeMount      `json:"mounts"`
			Secret *corev1.SecretKeySelector `json:"secret"`
			Init   bool                      `json:"init"`
		}
		if err := json.Unmarshal(data, &input); err != nil {
			fuzztest.New(data).Fill(&input)
		}
		input.Agent.Name, input.Agent.Namespace = "agent", "default"
		ConfigReloaderContainer(ConfigReloaderOpts{
			CR: &input.Agent, Mounts: input.Mounts, SecretKeySelector: input.Secret, IsInit: input.Init,
		})
	})
}

func FuzzStorageVolumeMounts(f *testing.F) {
	for _, seed := range []string{
		`{}`,
		`{"storagePath":"/storage","dataVolumeName":"data"}`,
		`{"storagePath":"/storage","dataVolumeName":"data","pvc":true,"stateful":true}`,
		`{"storagePath":"/storage","dataVolumeName":"data","mounts":[{"name":"custom","mountPath":"/storage"}]}`,
		`{"storagePath":"/storage","dataVolumeName":"data","volumes":[{"name":"data","emptyDir":{}}],"pvc":true}`,
		`{"storagePath":"/storage","dataVolumeName":"data","mounts":[{"name":"data","mountPath":"/storage"}],"volumes":[{"name":"data","emptyDir":{}}]}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var input struct {
			Volumes        []corev1.Volume      `json:"volumes"`
			Mounts         []corev1.VolumeMount `json:"mounts"`
			StoragePath    string               `json:"storagePath"`
			DataVolumeName string               `json:"dataVolumeName"`
			PVC            bool                 `json:"pvc"`
			Stateful       bool                 `json:"stateful"`
		}
		if err := json.Unmarshal(data, &input); err != nil {
			fuzztest.New(data).Fill(&input)
		}
		var pvc *corev1.PersistentVolumeClaimVolumeSource
		if input.PVC {
			pvc = &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "data"}
		}
		_, _, _ = StorageVolumeMountsTo(input.Volumes, input.Mounts, pvc, input.StoragePath, input.DataVolumeName, input.Stateful)
	})
}

func FuzzProbeLifecycle(f *testing.F) {
	for _, seed := range []string{
		`{}`,
		`{"cr":{"path":"/health","scheme":"HTTP","port":"8429","liveness":true}}`,
		`{"cr":{"path":"/health","scheme":"HTTPS","port":"8429"}}`,
		`{"cr":{"scheme":"HTTP","port":"http","proxy":true,"liveness":true}}`,
		`{"cr":{"port":"8429"},"params":{"livenessProbe":{"httpGet":{"path":"/live"}},"readinessProbe":{"tcpSocket":{}},"startupProbe":{"exec":{"command":["true"]}}}}`,
		`{"cr":{"scheme":"HTTPS","port":"8429"},"params":{"readinessProbe":{"periodSeconds":1,"failureThreshold":2,"timeoutSeconds":3,"successThreshold":4}}}`,
		`{"cr":{"port":"-1"},"params":{"livenessProbe":{}}}`,
		`{"minor":30,"params":{"preStopSleepSeconds":10}}`,
		`{"minor":30,"params":{"preStopSleepSeconds":10,"terminationGracePeriodSeconds":10}}`,
		`{"minor":30,"params":{"preStopSleepSeconds":-1}}`,
		`{"minor":30,"params":{"preStopSleepSeconds":5},"container":{"lifecycle":{"preStop":{"exec":{"command":["sh"]}}}}}`,
		`{"minor":29,"params":{"preStopSleepSeconds":5}}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var input struct {
			CR        probeStub                   `json:"cr"`
			Params    *vmv1beta1.CommonAppsParams `json:"params"`
			Container corev1.Container            `json:"container"`
			Minor     uint64                      `json:"minor"`
		}
		if err := json.Unmarshal(data, &input); err != nil {
			fuzztest.New(data).Fill(&input)
		}
		major, minor := k8stools.ServerMajorVersion, k8stools.ServerMinorVersion
		t.Cleanup(func() { k8stools.ServerMajorVersion, k8stools.ServerMinorVersion = major, minor })
		k8stools.ServerMajorVersion, k8stools.ServerMinorVersion = 1, input.Minor
		Probe(&input.Container, input.CR, input.Params)
		Lifecycle(&input.Container, input.Params)
	})
}

func FuzzExtraArgsOverride(f *testing.F) {
	for _, seed := range []string{
		`{}`,
		`{"args":["-a=1","-b"],"extra":{"c":"3"},"dashes":"-"}`,
		`{"args":["-a=1","-b=2","-c"],"extra":{"a":"x","c":"y"},"dashes":"-"}`,
		`{"args":["--a=1","--b"],"extra":{"a":"x","-b":"y"},"dashes":"--"}`,
		`{"args":["-rule=a"],"extra":{"rule":"/etc/rules/*.yml"},"dashes":"-"}`,
		`{"args":["-=1","=2","","-"],"extra":{"":"x","=":"y"},"dashes":"-"}`,
		`{"args":["a=1"],"extra":{"a":"x"},"dashes":""}`,
		`{"args":["---a=1"],"extra":{"-a":"x","-":"y"},"dashes":"--"}`,
		`{"args":null,"extra":{"a":"x"},"dashes":"-"}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var input struct {
			Args   []string          `json:"args"`
			Extra  map[string]string `json:"extra"`
			Dashes string            `json:"dashes"`
		}
		if err := json.Unmarshal(data, &input); err != nil {
			fuzztest.New(data).Fill(&input)
		}
		AddExtraArgsOverrideDefaults(input.Args, input.Extra, input.Dashes)
	})
}
