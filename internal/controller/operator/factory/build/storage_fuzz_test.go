package build

import (
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

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
		if len(data) > 4096 {
			t.Skip()
		}
		var input struct {
			Volumes        []corev1.Volume      `json:"volumes"`
			Mounts         []corev1.VolumeMount `json:"mounts"`
			StoragePath    string               `json:"storagePath"`
			DataVolumeName string               `json:"dataVolumeName"`
			PVC            bool                 `json:"pvc"`
			Stateful       bool                 `json:"stateful"`
		}
		if err := json.Unmarshal(data, &input); err != nil {
			return
		}
		var pvc *corev1.PersistentVolumeClaimVolumeSource
		if input.PVC {
			pvc = &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "data"}
		}
		assertStableBuilder(t, func() (any, error) {
			volumes, mounts, err := StorageVolumeMountsTo(input.Volumes, input.Mounts, pvc, input.StoragePath, input.DataVolumeName, input.Stateful)
			return struct {
				Volumes []corev1.Volume
				Mounts  []corev1.VolumeMount
			}{volumes, mounts}, err
		})
	})
}
