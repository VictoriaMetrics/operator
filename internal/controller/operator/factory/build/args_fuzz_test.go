package build

import (
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func FuzzLicenseBuilders(f *testing.F) {
	for _, seed := range []string{
		`{}`,
		`{"license":{"key":"enterprise-key"},"args":["-storageDataPath=/data"]}`,
		`{"license":{"keyRef":{"name":"license-secret","key":"license"},"forceOffline":true,"reloadInterval":"1h"},"mountDir":"/etc/licenses"}`,
		`{"license":{"key":"key","keyRef":{"name":"secret","key":"key"}},"volumes":[{"name":"extra","emptyDir":{}}],"mounts":[{"name":"extra","mountPath":"/extra"}]}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 4096 {
			t.Skip()
		}
		var input struct {
			License  *vmv1beta1.License   `json:"license"`
			Args     []string             `json:"args"`
			MountDir string               `json:"mountDir"`
			Volumes  []corev1.Volume      `json:"volumes"`
			Mounts   []corev1.VolumeMount `json:"mounts"`
		}
		if err := json.Unmarshal(data, &input); err != nil {
			return
		}
		assertStableBuilder(t, func() (any, error) {
			volumes, mounts := LicenseVolumeTo(input.Volumes, input.Mounts, input.License, input.MountDir)
			return struct {
				Args       []string
				DoubleArgs []string
				Volumes    []corev1.Volume
				Mounts     []corev1.VolumeMount
			}{LicenseArgsTo(input.Args, input.License, input.MountDir), LicenseDoubleDashArgsTo(input.Args, input.License, input.MountDir), volumes, mounts}, nil
		})
	})
}

func FuzzStreamAggrRelabelArgs(f *testing.F) {
	for _, seed := range []string{
		`{}`,
		`{"streamAggr":[{"dedupInterval":"1m","dropInputLabels":["pod","instance"]}]}`,
		`{"streamAggr":[{"rules":[{"match":"up","interval":"1m","outputs":["total"]}],"keepInput":true},{"dedupInterval":"30s"}]}`,
		`{"relabel":[{"inlineRelabelConfig":[{"targetLabel":"job","replacement":"vmagent"}]},null,{"relabelConfig":{"name":"relabel","key":"rules.yaml"}}]}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 4096 {
			t.Skip()
		}
		var input struct {
			Args       []string                         `json:"args"`
			StreamAggr []*vmv1beta1.StreamAggrConfig    `json:"streamAggr"`
			Relabel    []*vmv1beta1.CommonRelabelParams `json:"relabel"`
		}
		if err := json.Unmarshal(data, &input); err != nil {
			return
		}
		if len(input.StreamAggr) > 8 || len(input.Relabel) > 8 {
			t.Skip()
		}
		streamKeys := make([]string, len(input.StreamAggr))
		for i := range streamKeys {
			streamKeys[i] = "stream.yaml"
		}
		relabelKeys := make([]string, len(input.Relabel))
		for i := range relabelKeys {
			relabelKeys[i] = "relabel.yaml"
		}
		assertStableBuilder(t, func() (any, error) {
			return struct {
				StreamAggr []string
				Relabel    []string
			}{StreamAggrArgsTo(input.Args, "streamAggr", streamKeys, input.StreamAggr...), RelabelArgsTo(input.Args, "remoteWrite.relabelConfig", relabelKeys, input.Relabel...)}, nil
		})
	})
}
