package build

import (
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/VictoriaMetrics/operator/api/operator/fuzztest"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

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
