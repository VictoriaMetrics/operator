package build

import (
	"context"
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/VictoriaMetrics/operator/api/operator/fuzztest"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func FuzzVMBackupContainers(f *testing.F) {
	for _, seed := range []string{
		`{}`,
		`{"backup":{"acceptEULA":true,"port":"8013","destination":"s3://backup/path"},"port":"8428","storagePath":"/storage"}`,
		`{"backup":{"acceptEULA":true,"port":"8013","destination":"s3://backup/","extraArgs":{"a":"1","b":"2"},"credentialsSecret":{"name":"creds","key":"config"}},"cluster":true,"extraArgs":{"custom":"value"}}`,
		`{"backup":{"port":"8013","destination":"gs://bucket","extraEnvs":[{"name":"CUSTOM","value":"test"}]},"license":{"key":"test"}}`,
		`{"backup":{"port":"8013","snapshotCreateURL":"http://snapshot/create","snapshotDeleteURL":"http://snapshot/delete","volumeMounts":[{"name":"config","mountPath":"/etc/config"}]},"mounts":[{"name":"storage","mountPath":"/storage"}]}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var input struct {
			Backup      vmv1beta1.VMBackup   `json:"backup"`
			License     *vmv1beta1.License   `json:"license"`
			Port        string               `json:"port"`
			StoragePath string               `json:"storagePath"`
			Mounts      []corev1.VolumeMount `json:"mounts"`
			ExtraArgs   map[string]string    `json:"extraArgs"`
			Cluster     bool                 `json:"cluster"`
		}
		if err := json.Unmarshal(data, &input); err != nil {
			fuzztest.New(data).Fill(&input)
		}
		_, _ = VMBackupManager(context.Background(), &input.Backup, input.Port, input.StoragePath, input.Mounts, input.ExtraArgs, input.Cluster, input.License)
		_, _ = VMRestore(&input.Backup, input.StoragePath, input.Mounts)
	})
}
