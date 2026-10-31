package build

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/VictoriaMetrics/operator/api/operator/fuzztest"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func assertStableBuilder(t *testing.T, render func() (any, error)) {
	t.Helper()
	first, err := render()
	second, nextErr := render()
	if (err == nil) != (nextErr == nil) || err != nil && err.Error() != nextErr.Error() {
		t.Fatalf("non-idempotent build errors: %v != %v", err, nextErr)
	}
	if err != nil {
		return
	}
	a, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatalf("non-idempotent build: %s != %s", a, b)
	}
}

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
		assertStableBuilder(t, func() (any, error) {
			manager, err := VMBackupManager(context.Background(), &input.Backup, input.Port, input.StoragePath, input.Mounts, input.ExtraArgs, input.Cluster, input.License)
			if err != nil {
				return nil, err
			}
			restore, err := VMRestore(&input.Backup, input.StoragePath, input.Mounts)
			return struct {
				Manager *corev1.Container
				Restore *corev1.Container
			}{manager, restore}, err
		})
	})
}
